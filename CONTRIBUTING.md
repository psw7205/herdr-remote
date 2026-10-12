# 개발 가이드

이 문서는 로컬 개발과 검증 절차를 설명한다. 제품 경계는 [AGENTS.md](AGENTS.md),
요구·결정은 [PRD](docs/prd.md)와 [ADR](docs/adr.md)를 따른다.

## 환경 준비

`mise.toml`의 Go/Node.js/pnpm version을 사용한다. 설치·기본 실행은
[README](README.md#빠른-시작)를 따른다. `web/pnpm-lock.yaml`을 유지하고 다른 package manager의
lockfile을 추가하지 않는다. Go dependency를 바꾸면 `go.mod`와 `go.sum`을 함께 확인한다.

Bridge는 시작할 때 `web/dist/index.html`을 검사한다. Vite 개발 모드에서도 최초에 한 번
`mise exec -- pnpm --dir web build`를 실행해야 한다.

## 개발 server

기존 Herdr server가 실행 중이고 필요한 API를 제공해야 한다. 첫 terminal에서:

```sh
export HERDR_SOCKET_PATH="$(herdr status server | sed -n 's/^socket: *//p')"
test -S "$HERDR_SOCKET_PATH"
mise exec -- go run ./cmd/bridge -herdr-socket "$HERDR_SOCKET_PATH" \
  -origins "http://127.0.0.1:8787,http://127.0.0.1:5173"
```

다른 terminal에서:

```sh
mise exec -- pnpm --dir web dev --strictPort
```

`http://127.0.0.1:5173`으로 접속한다. Vite는 `/api`와 WS를 `127.0.0.1:8787`로 proxy한다. `go run`으로 띄운
Bridge는 VCS stamp가 없어 세션 목록에 `dev`로 보이고, dev server의 page는 build id가 없어 새로고침 안내가 뜨지 않는다.
`localhost`와 `127.0.0.1`은 다른 Origin이므로 주소와 allowlist를 일치시킨다.
기존 Bridge가 port를 사용 중이면 해당 process/service를 확인한 뒤 개발용 instance와 조정한다.
Herdr server나 사용자 agent를 종료해서 port 문제를 해결하지 않는다.

### Fixture 화면

Herdr와 Bridge 없이 UI 상태를 확인할 때는 Vite만 띄우고 `http://127.0.0.1:5173/fixture.html`에
접속한다. `?scenario=`로 `long`, `live`, `disconnected`, `delivery-unknown`, `rejected`, `empty`,
`herdr-down`, `unsupported`, `slow`, `superseded`, `start-trust`, `start-unknown`, `no-root`, `codex`, `changes-large`,
`tools`, `stale-client`, `wide-table`을 고를 수 있다. `stale-client`는 설치된 client build가 열린 화면과 달라 새로고침 안내가 뜨는
상태다. `tools`는 작업 중 session에 tool group(접힘, 오류, 결과 없음, 잘린 결과)을 보이고, 실행 중인
tool이 몇 초 뒤 `message.updated`로 끝나는 흐름을 재생한다. 변경된 파일 화면은 모든 scenario에서 session마다 다른 상태(수정·추가·삭제·이름 변경·binary,
변경 없음, Git repository 아님, 조회 오류)를 보이고, `changes-large`는 파일 수와 diff 크기 상한에 걸린
화면을 보인다. `wide-table`은 열 12개짜리 넓은 표와 긴 code block을 보인다. `/`로 시작하는 입력은 transcript에서 숨는 slash command처럼 echo 없이 수락된다. 이 page는
fetch와 WebSocket만 가짜 Bridge로 바꾸고 실제 client code를 그대로 실행한다. 데이터는 `web/src/fixtures/`의 익명 sample뿐이며 production
build에는 포함되지 않는다. 실제 session으로 화면을 확인할 때는 screenshot을 추적 파일에 남기지 않는다.
dev server의 CSS는 build처럼 낮춰지지 않는다(`light-dark()` 등). 색이나 theme을 바꾸면 build한
화면에서도 확인한다.
`mise exec -- pnpm --dir web --silent measure:layout`은 `web/dist`와 별도로 fixture page를 임시 directory에
production mode로 build해 headless Chrome(기본은 macOS Google Chrome, `CHROME_BIN`으로 변경)으로 폭 360·390·412의 `wide-table` 표·code block 가로 scroll과 page 넘침을
측정하고 JSON을 stdout에 낸다. 실패하면 exit 1이다. `--silent`가 없으면 pnpm의 실행 banner가 stdout의 JSON 앞에 붙는다.

## 변경별 검증

| 변경 | 실행할 검증 |
| --- | --- |
| Go Bridge | `mise run test`, `mise run vet` |
| React/TypeScript/PWA | `mise exec -- pnpm --dir web test`, `mise exec -- pnpm --dir web build` |
| protocol·두 영역 공통 변경 | 위 네 명령 모두 |
| `scripts/`·`mise.toml` task | `mise exec -- shellcheck scripts/*.sh scripts/mobile-e2e/*.sh`, scratch `PREFIX`와 별도 LaunchAgent로 실제 task 실행 |
| 문서 | `git diff --check`, relative link·실제 명령·개인 경로 유입 확인 |
| Herdr 본체 | `herdr` repo가 요구하는 검증 및 affected runtime scenario |

`mise run test`는 `go test -race ./...`, `mise run vet`는 `go vet ./...`다.
Herdr가 없는 환경에서도 synthetic fixture 기반 unit/integration 테스트를 실행할 수 있다.
실제 socket 진단은 별도로 실행한다.

```sh
mise exec -- go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
```

GitHub Actions(`.github/workflows/ci.yml`)는 push와 pull request마다 Ubuntu runner에서 `mise.toml`의 version으로
`mise run vet`, `mise run test`, `shellcheck scripts/*.sh scripts/mobile-e2e/*.sh`, `pnpm --dir web install --frozen-lockfile`,
`pnpm --dir web test`, `pnpm --dir web build`를 실행한다. 위 표의 Go Bridge·React/TypeScript/PWA·protocol 행과
`scripts/`의 shellcheck가 CI 범위다. `measure:layout`(Chrome), `scripts/deploy.sh` task의 실제 실행, `doctor`,
격리 Herdr, 실기기 검증은 CI가 맡지 않으며 CI 성공으로 대체하지 않는다. workflow는 secret과 host별 값을 쓰지 않는다.

## 실제 handoff 검증

실행 전후의 pane, foreground process incarnation, native session identity를 기록한다.
사용자 transcript 원문을 test artifact나 Git에 저장하지 않는다.

1. 같은 cwd의 여러 session을 native ID로 구별한다.
2. 한 mobile prompt가 같은 process/session의 transcript에 user/assistant로 나타나는지 확인한다.
3. 같은 `command_id` retry와 stale binding을 보내 duplicate 또는 다른 process 입력이 없는지 확인한다.
4. WS/Bridge를 끊었다가 복구해 agent 지속, cursor epoch, replay 또는 새 snapshot을 확인한다.
5. unsupported interaction은 같은 Terminal에서 처리하고, mobile 화면 전환 전후 PTY geometry를 비교한다.

실행한 항목과 unit test로만 확인한 항목을 구분해 기록한다. 더 상세한 미완료 acceptance는
[`P0` issue](https://github.com/psw7205/herdr-remote/issues?q=is%3Aissue+is%3Aopen+label%3AP0)를 따른다.

## 격리 Herdr 환경

binding·dispatch·lifecycle을 바꾸거나 Herdr patch·binary를 전환할 때는 기존 사용자 server를
건드리지 않고 이름 있는 격리 server에서 실제 agent로 확인한다. 화면에 드러나는 UI 동작은
사용하면서 확인한다([검증 수준 원칙](AGENTS.md#검증과-완료)).

```sh
E2E="$(mktemp -d)"; mkdir "$E2E/proj"; git -C "$E2E/proj" init -q
# 격리 server. coding agent의 환경변수(HERDR_*, CLAUDE_CODE_*)를 물려주지 않는다.
cat > "$E2E/h" <<EOF
#!/bin/sh
exec env -i HOME="$HOME" USER="$USER" SHELL="$SHELL" TERM=xterm-256color LANG="${LANG:-en_US.UTF-8}" PATH="$PATH" herdr --session e2e "\$@"
EOF
chmod +x "$E2E/h"
nohup "$E2E/h" server > "$E2E/server.log" 2>&1 &
herdr session list                      # e2e의 socket 경로 확인
"$E2E/h" workspace create --cwd "$E2E/proj" --no-focus
"$E2E/h" agent start a --kind claude --pane <pane-id> -- --model <작은 model>
# Bridge는 별도 port·receipt와 tailnet off로 띄운다.
mise exec -- go run ./cmd/bridge -herdr-socket <e2e socket> -receipts-dir "$E2E/receipts" \
  -listen 127.0.0.1:8797 -origins http://127.0.0.1:8797 -tailnet-host off
# 새 session 시작을 볼 때는 임시 root를 함께 넘긴다: -project-root "$E2E"
```

- 모든 Herdr 명령은 `"$E2E/h"`로 실행한다. `--session` 없는 `herdr`나 상속된 `HERDR_SOCKET_PATH`는
  default server를 가리킨다. 시작 전에 default의 `herdr pane list`와 agent PID를 기록하고 끝난 뒤 비교한다.
- `env -i`를 빼면 Claude Code 안에서 띄운 server의 새 Claude가 transcript를 저장하지 않는다.
- 처음 여는 폴더는 신뢰 확인 화면에서 `agent start`가 `agent_not_ready`로 끝난다.
  `"$E2E/h" pane send-keys <pane-id> down enter`로 수락한 뒤 다시 시작한다.
- `agent start`는 사용자 shell에서 실행되므로 alias가 붙인 옵션이 적용된다. permission 대기를
  재현하려면 pane에 `command claude ...`를 직접 입력한다.
- native session은 첫 prompt 뒤 생긴다. Bridge `prompt`는 `claude:<native-id>` item에만 전달된다.
- Bridge로 시작한 session은 agent 인자가 없어 사용자 기본 model로 뜬다. 그 안에서 `/model`을 쓰면
  사용자 설정의 기본 model이 바뀌므로, 바꿨다면 정리할 때 원래 값으로 되돌린다.
- stock 전환은 설치 binary를 바꾸지 않고 `"$E2E/h" server live-handoff --import-exe <binary>`로 한다.
- 정리 순서: Bridge 종료 → `"$E2E/h" server stop` → `herdr session delete e2e` →
  Claude transcript root(Bridge `-claude-dir` 기본값) 아래 임시 directory 이름의 test transcript 삭제 →
  Claude 사용자 설정 `~/.claude.json`의 `projects`에서 임시 directory 경로 항목 삭제(신뢰 확인 수락이 남긴
  기록이다. 실행 중인 Claude도 이 파일을 쓰므로 백업 후 바꾸고 다시 확인한다) → default pane·PID 비교.
- 결과는 `docs/records/verification.md`에 날짜 section으로 남기고 native ID·binding은 placeholder로 쓴다.

## Android emulator 확인

issue가 "실기기"로 미룬 touch·keyboard·잠금·연결 끊김·PWA 동작은 이 Mac의 Android emulator로 대신 확인한다.
`scripts/mobile-e2e/`가 그 환경을 만든다. 격리 Herdr 환경(위 절)을 script로 묶은 것이라 같은 규칙이 적용된다.

```sh
scripts/mobile-e2e/herdr-e2e.sh up          # 격리 server·임시 project의 Claude·개발용 Bridge(8797). E2E_URL 출력
scripts/mobile-e2e/android.sh up            # AVD boot, adb reverse, Chrome에 E2E_URL, DevTools forward(9222)
node scripts/mobile-e2e/cdp.mjs center 'button[aria-label="위 화살표"]'   # device px 좌표
scripts/mobile-e2e/android.sh hold 980 2130 1000                         # 실제 touch 입력
node scripts/mobile-e2e/cdp.mjs eval 'document.title'                   # page 상태
scripts/mobile-e2e/android.sh down && scripts/mobile-e2e/herdr-e2e.sh down
```

- host별 값은 env로 준다: `ANDROID_SDK_ROOT`, `HERDR_E2E_AVD`(기본 Pixel_4_API_31), `HERDR_E2E_PORT`, `HERDR_E2E_MODEL`,
  `HERDR_E2E_DIR`. 기본 AVD는 Android 12·Chrome 120이다.
- `herdr-e2e.sh down`은 Bridge·server·session·test transcript·`~/.claude.json`의 임시 project 항목을 지우고,
  default server의 pane 목록과 `claude` PID를 `up` 시점과 대조해 출력한다. diff가 있으면 default server를 건드린 것이다.
- 확인하지 못하는 것: iOS의 탭·keyboard·PWA 설치(이 Mac의 Xcode에는 Simulator.app이 없어 Safari 캡처만 가능), 실제 phone의
  tailnet 경유 접속, 다른 tailnet 사용자 identity 거부, 실기기의 시간 값. 결과는 `docs/records/verification.md`에
  실측·unit test·미확인을 구분해 남긴다.

## 문서와 변경 제출

작업 시작 시 관련 issue와 성공 기준을 정한다. 작은 구현 선택은 code로 설명하고,
architecture 의미가 바뀔 때만 ADR을 수정한다. 실제 구현과 문서가 다르면 차이를 확인한 뒤
완료 상태를 갱신한다. 기능 아이디어를 구현 완료로 바꾸지 않는다.

검증된 논리 단위로 commit한다. commit subject는 `type(scope): 설명` 형식으로 작성한다.
coding agent로 작업할 때의 push·main integration 규칙은 [AGENTS.md](AGENTS.md)를 따른다.

## Issue와 Pull Request

개인 project이며 응답과 merge는 보장하지 않는다. 범위는 single-user, single-host다.
multi-user, public internet 노출, 자체 agent runner, conversation DB 같은
[PRD non-goal](docs/prd.md#5-non-goals)과 [Explicitly Deferred](docs/prd.md#32-explicitly-deferred)는 받지 않는다.

- 보안 취약점은 issue로 올리지 않고 [SECURITY.md](SECURITY.md)의 비공개 경로로 신고한다.
- bug report에는 재현 절차, 이 repo의 commit, Herdr patch commit, Claude Code version을 적는다.
  `doctor` 출력은 [진단 출력 공유](README.md#진단-출력-공유)처럼 식별 정보를 placeholder로 바꾼다.
- transcript 원문, 실제 prompt, binding token, screenshot의 실제 session 내용을 issue·PR·fixture에
  넣지 않는다. fixture는 실제 구조를 확인한 익명 sample로 만든다.
- PR은 한 가지 변경에 집중하고, 위 [변경별 검증](#변경별-검증)을 통과시키고, 관련 issue를 `#N`으로 적는다.
  [AGENTS.md](AGENTS.md)의 제품 불변식을 바꾸는 변경은 먼저 issue로 논의한다.
- Herdr 조건부 입력 patch의 변경은 이 repo가 아니라 fork `psw7205/herdr`의 `mobile-binding` branch를
  대상으로 한다([Herdr patch runbook](docs/herdr-patch.md#8-fork-관리)).
