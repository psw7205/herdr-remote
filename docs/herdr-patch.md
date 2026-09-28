# Herdr patch 유지 runbook

2026-09-28 기준. Backlog [P0-04](backlog.md#p0--배포와-핵심-안정성)의 운영 절차다.
Herdr 연동 근거는 [Integration 조사](records/integration-findings.md), 실제 검증 기록은
[검증 기록](records/verification.md)을 따른다.

각 단계의 상태 표기는 다음과 같다.

| 표기 | 의미 |
| --- | --- |
| **실측** | 작성자의 macOS host에서 실제로 실행해 결과를 확인했다 |
| **code 확인** | `herdr` repo source와 설정을 읽어 확인했으나 이 절차로 실행하지 않았다 |
| **미검증** | 실행하지 않았다. 격리 환경에서 검증하기 전에는 기대 동작일 뿐이다 |

경로 placeholder:

| Placeholder | 의미 |
| --- | --- |
| `<herdr-repo>` | fork `psw7205/herdr` checkout. `origin`=fork, `upstream`=`herdrdev/herdr` |
| `<patch-worktree>` | `mobile-binding` branch를 checkout한 `herdr` worktree |
| `<install-dir>` | 설치된 `herdr` binary의 directory. `dirname "$(command -v herdr)"`로 확인한다 |

## 1. patch가 필요한 이유와 fail-closed 범위

Bridge의 Chat prompt, interrupt, Terminal 입력은 Herdr socket method `agent.binding`과
`agent.bound_input`이 있어야 한다([ADR-034](adr.md#adr-034--herdr가-runtime-binding을-검증한-command만-전달한다)).
두 method는 fork [`psw7205/herdr`의 `mobile-binding` branch](https://github.com/psw7205/herdr/tree/mobile-binding),
commit `0e672c5e`에만 있다.
stock `0.9.1`에는 없다. patch는 macOS Claude foreground process의 PID·시작 시각·native
session metadata를 대조해 binding을 발급하고, PTY queue가 text와 Enter를 쓰기 직전에도
binding을 다시 검증한다.

patch가 없거나 binding을 확인할 수 없으면 Bridge는 fail closed한다.

- Chat 입력, interrupt, Terminal 화면과 입력이 비활성화된다.
- Bridge가 raw pane input이나 stock `agent.prompt`로 우회하지 않는다.
- Herdr가 소유한 agent process는 영향을 받지 않는다.

binding이 사라져도 agent가 같은 pane에서 계속 보고되면 기존 `claude:<native-id>` session은
`ended`가 아닌 `unverified`로 남고, 같은 native session이 다시 검증되면 `active`로 돌아간다.
native session을 식별하지 못한 `pane:<pane-id>` item은 `unbound`로 보인다
([P0-07](backlog.md#p0--배포와-핵심-안정성), ADR-034).

## 2. 현재 상태 확인

stock과 patched binary는 모두 `herdr 0.9.1`을 출력한다. `herdr --version`과
`herdr status server`의 version으로 patch 여부를 판단하지 않는다.

```sh
export HERDR_SOCKET_PATH="$(herdr status server | sed -n 's/^socket: *//p')"
mise exec -- go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
curl -s http://127.0.0.1:8787/api/sessions
```

| 신호 | 위치 | 의미 |
| --- | --- | --- |
| `conditional_input` | `doctor` JSON | `supported`, `unsupported`, `unknown` |
| `blockers` | `doctor` JSON | `unsupported`이면 이 문서를 가리키는 blocker가 포함된다 |
| `verified_binding` | `doctor`의 agent별 항목 | 해당 pane의 binding 검증 성공 여부 |
| `herdr.conditional_input` | `/api/sessions` 응답 | Bridge가 관찰한 현재 server capability |
| 안내 문구 | Web UI | `unsupported`이면 입력 불가 이유를 표시한다 |

- `supported`: 실행 중 server가 조건부 입력 method를 제공한다. 모든 pane의 binding 성공을
  뜻하지는 않으므로 `verified_binding`도 확인한다.
- `unsupported`: 실행 중 server에 method가 없다. stock binary로 바뀌었을 가능성이 높다.
  아래 설치 절차를 따른다.
- `unknown`: 판정 근거가 없다. claude agent가 없거나, 모든 `agent.binding` 조회가
  transport·응답 형식 오류로 끝났거나, Bridge가 아직 한 번도 Refresh에 성공하지 못한 경우다.
  `herdr status server`와 Bridge log를 먼저 본다.

Refresh에 한 번 성공한 뒤의 `session.snapshot` 실패는 값을 바꾸지 않는다. Bridge는 직전 성공
Refresh의 값을 유지하므로 `/api/sessions`의 값이 실제 server 상태보다 늦을 수 있다.
`doctor`는 socket 조회가 실패하면 JSON을 출력하지 않고 error로 종료한다.

patch→stock handoff 직후처럼 다음 2초 Refresh가 binding을 지우기 전에 보낸 command는
`agent.bound_input` method 자체가 거부된다. Herdr는 이를 request deserialization 단계, 즉 PTY
write 전에 거부하므로 Bridge는 `delivery_unknown`이 아니라 `rejected`·`HERDR_UNSUPPORTED`로
receipt에 기록하고, 같은 `command_id` 재시도에는 재전송 없이 이 결과를 돌려준다. Chat UI는
conditional input API가 없어 입력을 전달하지 않았다는 안내를 표시한다.

상태: stock server를 상대로 `unsupported`가 표시되는지는 **미검증**이다. `doctor`의
"No verified native runtime binding" blocker와 `conditional_input` blocker는 synthetic fixture
test로 확인했다. `doctor` 성공은 capability 관찰이며 PTY handoff나 race 통과를 뜻하지 않는다.

## 3. patch build

`<patch-worktree>`에서 실행한다. branch는 `v0.9.1`(`065ef9d6`) 위의 단일 commit
`0e672c5e`다. 새로 준비할 때는 fork를 clone한다.

```sh
git clone --branch mobile-binding https://github.com/psw7205/herdr.git <patch-worktree>
```

```sh
git -C <patch-worktree> status --short
git -C <patch-worktree> log --oneline -2
just ci
just build
```

- `just ci`는 `just lint`(`cargo fmt --check`, `cargo clippy --all-targets --locked -- -D warnings`)와
  `just ci-tests`(`cargo nextest`, maintenance·UI hot-path·integration asset test)를 실행한다.
- `just build`는 `cargo build --release --locked`이며 결과는 `target/release/herdr`다.
  `CARGO_TARGET_DIR`을 쓰면 그 directory 아래에 생긴다.
- 필요한 도구는 `herdr` repo의 `rust-toolchain.toml`, `justfile`, `CONTRIBUTING.md`를 따른다.
  `just`, `cargo-nextest`, `python3`, `bun`이 필요하다.

상태: **실측**. 2026-09-23 `just ci`에서 3,463개 테스트와 release build가 통과했다.
설치된 `herdr`의 sha256은 `<patch-worktree>/target/release/herdr`와 같다.

## 4. stock binary 백업과 patch 설치

최초 설치 전 stock binary를 같은 directory에 `<install-dir>/herdr-remote-original-0.9.1`로
남긴다. 이미 있는 백업은 덮어쓰지 않는다.

```sh
INSTALL_DIR="$(dirname "$(command -v herdr)")"
# 최초 1회. 현재 binary가 stock일 때만 실행한다.
test -e "$INSTALL_DIR/herdr-remote-original-0.9.1" \
  || cp -p "$INSTALL_DIR/herdr" "$INSTALL_DIR/herdr-remote-original-0.9.1"

cp -p <patch-worktree>/target/release/herdr "$INSTALL_DIR/herdr.patch-new"
mv "$INSTALL_DIR/herdr.patch-new" "$INSTALL_DIR/herdr"
shasum -a 256 "$INSTALL_DIR/herdr" <patch-worktree>/target/release/herdr
```

- 같은 directory 안의 `mv`는 원자적 rename이며 Herdr updater도 같은 방식으로 교체한다.
  실행 중 server는 이전 binary로 계속 동작한다. 새 binary는 다음 단계에서 적용한다.
- 백업과 설치본의 sha256을 기록한다. version 문자열로는 백업이 stock인지 구별할 수 없다.
  그 binary로 띄운 격리 server에서 `conditional_input: unsupported`를 확인하는 방법이 있다.
- 설치된 binary는 Git 밖의 host 상태다. repo 변경과 구분해 기록한다.

상태: 백업과 설치는 **실측**. 백업 명령의 정확한 실행 형태는 기록되지 않았다.

## 5. 실행 중 server에 적용: live handoff

Herdr server를 종료하면 pane과 agent process도 끝난다. `herdr server stop`이나 재시작으로
적용하지 않는다. Herdr의 live handoff는 새 binary로 import server를 띄워 기존 pane과 PTY를
넘기고 이전 server만 종료한다.

```sh
herdr status server          # status: running 확인
ps -axo pid,lstart,comm | grep -i claude   # 전후 비교용 기록
herdr server live-handoff --import-exe "$INSTALL_DIR/herdr"
herdr status server
mise exec -- go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
```

- `herdr server live-handoff [--import-exe <path>] [--expected-protocol <n>] [--expected-version <version>]`는
  socket method `server.live_handoff`를 보낸다. `--import-exe`가 없으면 실행 중 server의
  `current_exe()`를 사용한다. 교체 대상을 명확히 하기 위해 `--import-exe`를 지정한다.
- `herdr update --handoff`도 같은 live handoff 경로를 쓴다. 이 명령은 stock을 내려받아 설치하므로
  patch 적용에 사용하지 않는다(§7).
- 실행 전후 [CONTRIBUTING.md](../CONTRIBUTING.md#실제-handoff-검증)의 항목대로 pane,
  foreground process incarnation, native session identity를 기록한다.

알려진 영향:

| 항목 | 결과 |
| --- | --- |
| agent PID·native session ID·shell PID | 보존됐다 |
| terminal ID | 새로 발급돼 기존 binding이 무효화된다. Bridge가 새 binding을 다시 조회해야 한다 |
| pane geometry | desktop client가 끊긴 동안 기본 120×40으로 바뀌었다. desktop client가 다시 붙으면 크기 소유권을 되찾는다([P1-08](backlog.md#p1--핵심-ux와-다음-agent)) |
| mobile Terminal | resize를 호출하지 않는다. geometry 변화는 handoff 자체의 동작이다 |

상태: stock → patch handoff는 **실측**. Herdr server log에 live handoff 시작, import server
생성, pane 4개 import, 이전 server 종료가 기록됐다. 두 Claude PID와 native session ID가
유지됐다. 실행한 CLI 인자는 기록되지 않았다. patch → patch 재적용은 **미검증**이다.

## 6. 원본 binary로 rollback

```sh
INSTALL_DIR="$(dirname "$(command -v herdr)")"
cp -p "$INSTALL_DIR/herdr" "$INSTALL_DIR/herdr-remote-patched-0.9.1"   # patch 보존
cp -p "$INSTALL_DIR/herdr-remote-original-0.9.1" "$INSTALL_DIR/herdr.rollback-new"
mv "$INSTALL_DIR/herdr.rollback-new" "$INSTALL_DIR/herdr"
herdr server live-handoff --import-exe "$INSTALL_DIR/herdr"
mise exec -- go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
```

기대 결과:

- agent process는 유지되고 `conditional_input`은 `unsupported`가 된다.
- Bridge는 Chat 입력과 Terminal을 fail closed하고 UI에 안내를 표시한다.
- 열린 `claude:` session은 `ended`가 아닌 `unverified`로 남고 입력과 Terminal이 fail closed한다
  (P0-07). agent 종료로 해석하지 않는다.

상태: **미검증**. patch server → stock import의 handoff 호환성, `unsupported` 판정,
UI 안내는 실행해 보지 않았다. 사용자 agent가 있는 server에서 처음 시도하지 않는다.
[AGENTS.md](../AGENTS.md#herdr-및-실제-runtime-작업)에 따라 별도 이름의 격리 Herdr server에서
먼저 검증하고 생성한 테스트 자원을 정리한다.

## 7. upgrade 정책

`herdr update`를 그대로 실행하지 않는다.

- Herdr의 background check는 새 version을 알리기만 한다. 수동 `herdr update`는 stable
  channel의 binary를 내려받아 설치 경로에 rename한다(`src/update.rs`).
- stable channel은 최신 version이 현재 version보다 높을 때만 설치한다. patch도 `0.9.1`로
  보고하고 최신 tag가 `v0.9.1`이므로 지금은 설치할 update가 없다. 다음 stable release 이후
  `herdr update`(또는 `--handoff`)를 실행하면 patch가 stock으로 교체된다. 자동 설치는 없다.
- `herdr update --handoff`는 교체 후 live handoff까지 수행한다. agent는 살아 있지만 조건부
  입력만 조용히 사라진다. `conditional_input` 감지가 이 경우를 드러내는 신호다.
- Herdr 설정의 `[update]`에는 `channel` 외에 `version_check`, `manifest_check` key가 있으나
  background 확인을 끄는 설정이다. 수동 `herdr update`를 막는 설정은 확인되지 않았다
  (**code 확인**).

새 Herdr release를 반영하는 절차:

```sh
git -C <herdr-repo> fetch upstream --tags
git -C <patch-worktree> rebase --onto <new-tag> v0.9.1 mobile-binding
# <patch-worktree>에서
just ci
just build
```

1. `<new-tag>`로 rebase하거나 새 branch에 `0e672c5e`를 cherry-pick한다. 충돌은 patch가
   수정한 아래 `herdr` repo 파일에서 날 수 있다.
   - 추가: `src/runtime_binding.rs`, `src/app/api/bound_input.rs`
   - API schema·server: `src/api/mod.rs`, `src/api/schema.rs`, `src/api/schema/agents.rs`,
     `src/api/schema/response.rs`, `src/api/server.rs`, `src/app/api.rs`
   - PTY·pane: `src/pty/actor.rs`, `src/pty/actor/unix.rs`, `src/pane.rs`,
     `src/terminal/runtime.rs`
   - platform·진입점: `src/platform/mod.rs`, `src/platform/macos.rs`, `src/main.rs`,
     `src/server/headless.rs`
   - 문서: `docs/next/api/herdr-api.schema.json`,
     `docs/next/website/src/content/docs/socket-api.mdx`
2. `just ci`와 `just build`를 통과시킨다.
3. §4의 절차로 설치한다. 이전 patched binary를 별도 이름으로 남긴다.
4. §5의 live handoff로 적용한다. private protocol version이 바뀌었으면 `herdr status server`의
   compatibility를 먼저 본다.
5. `doctor`에서 `conditional_input: supported`, 대상 agent의 `verified_binding: true`를
   확인한다.
6. stock backup도 새 version의 stock으로 갱신할지 결정하고 파일명에 version을 넣는다.

상태: **미검증**. rebase, 새 version build, version 간 handoff를 end-to-end로 실행한 적이 없다.

## 8. fork 관리

Herdr 원본은 승인되지 않은 외부 contributor의 feature PR을 받지 않으므로 patch는 fork
`psw7205/herdr`에서 유지한다([ADR-036](adr.md#adr-036--herdr-patch는-소유-fork에서-유지한다)).

| branch | 규칙 |
| --- | --- |
| `master` | `upstream/master`의 mirror. 자체 commit을 두지 않고 fast-forward로만 갱신한다 |
| `mobile-binding` | 최신 stable tag 위의 patch. README·runbook이 이 이름을 가리킨다 |

`master` sync:

```sh
git -C <herdr-repo> fetch upstream --tags
git -C <herdr-repo> switch master
git -C <herdr-repo> merge --ff-only upstream/master
git -C <herdr-repo> push origin master
```

새 stable release가 나오면 §7 절차로 `mobile-binding`을 rebase·검증한 뒤 push한다.

```sh
git -C <herdr-repo> tag mobile-binding-<old-tag> <old-head>
git -C <herdr-repo> push origin mobile-binding-<old-tag>
git -C <herdr-repo> push --force-with-lease origin mobile-binding
```

- rebase된 branch push는 공개 history를 바꾸므로 매번 소유자가 확인한다. 이전 head는
  `mobile-binding-<old-tag>` tag로 남겨 이미 설치한 사용자가 재현할 수 있게 한다.
- `master` sync는 patch branch와 독립적이다. stable release가 없으면 `mobile-binding`은 그대로 둔다.
