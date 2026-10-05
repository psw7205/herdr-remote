# Herdr Mobile Chat

> **English summary.** Herdr Mobile Chat (`herdr-remote`) is a single-user companion for
> [Herdr](https://github.com/herdrdev/herdr). It lets you read and continue Claude Code sessions
> that are already running in Herdr panes from a phone browser, over localhost or Tailscale Serve.
> The Bridge never runs or resumes agents itself: Herdr owns the processes and PTYs, new sessions
> are started only by asking Herdr, and chat history is read from Claude Code's native transcripts. It requires macOS, Claude Code, and a Herdr build
> with the conditional input patch ([`psw7205/herdr`](https://github.com/psw7205/herdr/tree/mobile-binding),
> branch `mobile-binding`); stock Herdr lacks the required API. Codex CLI sessions are shown as
> read-only chat when Herdr's Codex hook reports their session; Codex input is not supported yet.
> This is a personal project, not affiliated with Herdr. The documentation is in Korean.

Herdr에서 이미 실행 중인 coding agent를 모바일 브라우저에서 확인하고 같은 session에
입력하는 single-user, single-host client다. Herdr가 process와 PTY를 소유하고,
Chat은 native transcript를 읽어 표시한다. Bridge는 agent를 직접 실행하거나 resume하지 않는다.
repo와 Go module 이름은 `herdr-remote`다.

[Herdr](https://github.com/herdrdev/herdr)는 여러 coding agent를 terminal pane에서 실행하고
관리하는 도구다. 이 repo는 Herdr의 공식 project가 아닌 개인 companion tool이다.

<p align="center">
  <img src="docs/images/sessions.png" width="240" alt="상태별로 묶인 session 목록">
  <img src="docs/images/chat.png" width="240" alt="작업 중인 session의 Chat 화면">
  <img src="docs/images/terminal.png" width="240" alt="같은 pane의 Terminal 화면과 특수 키">
</p>

화면은 익명 sample을 쓰는 dev 전용 fixture page(`web/fixture.html`)에서 캡처했다.

## 현재 지원 범위

| 항목 | 상태 |
| --- | --- |
| Claude Code / macOS | 기존 session 발견, Chat, prompt, interrupt, Terminal fallback |
| 대화 복구 | incremental JSONL, snapshot/replay/live, Bridge 재시작 뒤 복구 |
| 입력 보호 | Herdr binding 검증, durable command receipt, 같은 command 재전송 방지 |
| 모바일 UI | 상태별 session 목록, Chat, light/dark theme, code 복사, 특수 키가 있는 Terminal, PWA. 실기기 keyboard와 설치형 PWA는 미검증 |
| Tailnet | Serve 경유 소유자 phone에서 Chat·prompt·Terminal 확인. 화면 잠금·네트워크 전환 뒤 reconnect와 다른 사용자 거부의 실측은 미검증 |
| Codex CLI | Herdr Codex hook이 보고한 session의 rollout을 read-only Chat으로 표시(ADR-038). prompt·interrupt·Terminal은 미지원. 실제 Herdr Codex pane에서는 미실측 |
| 새 session 생성 | `-project-root` 아래 Git repo의 새 workspace나 열린 workspace의 새 tab에서 Herdr로 Claude를 시작(ADR-037). 격리 Herdr에서 실측, 실기기는 사용 중 확인 |
| 변경된 파일 | session 폴더의 Git 변경 목록, +/− 줄 수, 파일별 diff를 read-only로 표시(ADR-016). 수정·commit·push는 없다. Bridge host에 git 2.31 이상이 필요하다 |
| Tool card | Claude tool 호출을 Chat에 compact group으로 보이고 입력·결과를 펼쳐 본다(ADR-039). 상세는 8 KiB까지. Codex는 미지원. 실제 session에서는 미실측 |
| Attachment·notification | 미구현 |

Claude의 localhost handoff와 Bridge 재시작 복구는 실제 process에서 검증했다.
미검증 scenario와 우선순위는 [GitHub Issues](https://github.com/psw7205/herdr-remote/issues)에 있다.

## 기술 스택

- Frontend: React + TypeScript + Vite, pnpm
- Bridge: Go `net/http`, `coder/websocket`, `fsnotify`, `log/slog`
- Terminal: xterm.js의 visible ANSI snapshot 표시
- Toolchain: `mise.toml`
- Persistence: command receipt만 disk에 보존. conversation DB·Redis·broker 없음

## 사전 조건

| 항목 | 조건 |
| --- | --- |
| OS·agent | macOS에서 Herdr pane으로 실행 중인 Claude Code interactive session. Codex CLI의 read-only Chat에는 Herdr Codex integration hook이 필요하다(`herdr integration status`로 확인) |
| Herdr | 조건부 입력 patch가 적용된 Herdr server가 실행 중이어야 한다. 아래 설명 참조 |
| Toolchain | [`mise`](https://mise.jdx.dev). Go·Node.js·pnpm version은 `mise.toml`이 고정한다 |
| Tailscale | 모바일 원격 접속에만 필요하다. Bridge host와 mobile device가 같은 tailnet에 로그인돼 있어야 한다 |

Bridge는 Herdr가 꺼졌을 때 대신 시작하지 않는다. stock Herdr에는 필수 API인
`agent.binding`과 `agent.bound_input`이 없다. 필요한 patch는 fork
[`psw7205/herdr`의 `mobile-binding` branch](https://github.com/psw7205/herdr/tree/mobile-binding)
(`v0.9.3` 위의 단일 commit `de31ede9`)다. build·설치·rollback·upgrade 절차는
[Herdr patch runbook](docs/herdr-patch.md)을 따른다. stock과 patched binary가 같은 version
문자열을 사용할 수 있으므로 version이 아니라 `doctor`로 확인한다. Herdr updater가 stock
binary를 설치하면 조건부 입력이 비활성화되며 Bridge는 입력을 fail closed한다.

## 빠른 시작

먼저 localhost에서 동작을 확인한다.

1. patched Herdr를 준비한다. [Herdr patch runbook](docs/herdr-patch.md)의 §3–5(build, stock
   binary 백업·설치, live handoff)를 따른다. 실행 중인 Herdr server를 종료하지 않는다.
2. 저장소 root에서 client를 build하고 `doctor`로 확인한 뒤 Bridge를 실행한다.

   ```sh
   mise install
   mise exec -- pnpm --dir web install --frozen-lockfile
   mise exec -- pnpm --dir web build

   export HERDR_SOCKET_PATH="$(herdr status server | sed -n 's/^socket: *//p')"
   test -S "$HERDR_SOCKET_PATH"
   mise exec -- go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
   mise exec -- go run ./cmd/bridge -herdr-socket "$HERDR_SOCKET_PATH"
   ```

3. 같은 machine의 브라우저에서 [http://127.0.0.1:8787](http://127.0.0.1:8787)로 접속한다.

`doctor`는 read-only이며 transcript 원문이나 binding token을 출력하지 않는다. 종료 코드 0은
조회 성공이고, 모든 handoff acceptance의 완료를 의미하지 않는다. `blockers`가 비어 있는지,
`conditional_input`이 `supported`이고 대상 agent의 `verified_binding`이 참인지 확인한다.
Tailnet을 아직 설정하지 않았어도 blocker는 아니다. localhost 전용 사용도 지원 범위다.

Vite hot reload와 변경별 검증 절차는 [CONTRIBUTING.md](CONTRIBUTING.md)에 있다.

## Tailnet 접속

원격 경로는 Tailscale Serve 하나다([ADR-024](docs/adr.md#adr-024--tailnet을-primary-security-boundary로-사용한다)).
Bridge는 항상 loopback에만 bind하고, Serve가 tailnet HTTPS 요청을 Bridge로 proxy한다.

1. Tailscale 관리 화면의 DNS 설정에서 MagicDNS와 **HTTPS Certificates**를 활성화한다.
   tailnet마다 한 번만 필요하다.
2. Bridge host에서 Serve를 켠다. `8787`은 Bridge `-listen` port다.

   ```sh
   tailscale serve --bg 8787
   ```

   tailnet에서 Serve를 처음 쓰면 CLI가 활성화 URL을 안내한다. Funnel은 켜지 않는다.
3. Bridge를 (재)시작한다. `-tailnet-host`와 `-tailnet-login`은 기본값 `auto`이므로 추가 flag가
   필요 없다. Bridge는 시작할 때 한 번 `tailscale status --json`을 읽어 host와 소유자 login을
   정하고, host와 login이 모두 확정된 경우에만 `https://<tailnet-host>`를 Origin allowlist에
   추가한다. 시작 log에 확정된 host·login과 그 출처가 남는다. Tailscale 상태를 바꾼 뒤에는
   Bridge를 재시작해야 반영된다.
4. `doctor`를 다시 실행해 `tailnet.issues`가 비어 있는지 확인한다. `issues`는 문제마다 조치
   방법을 적는다. Bridge `-listen`이 `127.0.0.1:8787`이 아니면 같은 주소를 `-bridge-listen`으로
   넘긴다. 그렇지 않으면 Serve proxy 확인(`serve_proxy`)이 거짓 음성이 된다. CLI 경로는 Bridge와
   같이 `-tailscale-bin`으로 명시할 수 있다. Funnel이 켜져 있거나, identity header 없이 Bridge에
   닿는 Serve TCP forward(`--tcp`/`--tls-terminated-tcp`)가 있으면 top-level blocker다. 둘 다
   `--bg` 없이 실행한 foreground 설정까지 확인하고, TCP forward는 `--service`로 만든 Tailscale
   Service 설정도 확인한다. MagicDNS host를 얻지 못해도 이 두 검사는 수행한다.
5. 소유자 계정으로 로그인한 mobile device에서 `https://<tailnet-host>`에 접속한다. 화면 잠금·
   네트워크 전환 뒤 WebSocket reconnect는 아직 실기기에서 검증하지 않았다.

Tailscale CLI가 없거나 실패하거나 Tailscale이 실행 중이 아니면 Bridge는 종료하지 않고
localhost 전용으로 동작한다. 단, `-tailnet-host`를 명시했는데 `auto` login만 확정하지 못했다면
host는 유지하되 모든 tailnet 요청을 거부한다. tagged node처럼 소유 사용자 login이 없을 때도
같다. 이때는 `-tailnet-login`으로 소유자를 명시한다. login이 없는 동안 `-origins`에 넣은
tailnet HTTPS Origin은 경고 log와 함께 제외되고 Bridge는 계속 시작한다.

### 보안 경계

Bridge는 Serve가 검증해 추가한 `Tailscale-User-Login`을 소유자 login과 대조한다. 다른
사용자나 누락된 identity는 읽기·제어 모두 거부한다. HTTP mutation과 WS는 exact Origin도
검사한다. Serve는 client의 `Host`를 그대로 넘기므로 Bridge는 `X-Forwarded-*`나
`Tailscale-User-*` header가 있는 요청을 `Host`와 무관하게 tailnet 요청으로 보고, tailnet host와
소유자 login이 확정되지 않았으면 거부한다. 그래서 Serve 외의 local reverse proxy를 앞에 두는
구성은 지원하지 않는다. 모든 응답은 frame 삽입을 금지한다. 자체 password/OAuth/JWT는 없다.
network 경계도 좁히도록 tailnet ACL/grant로 Bridge host의 HTTPS 접근을 필요한 device로 제한한다. [Serve identity 동작](https://tailscale.com/docs/features/tailscale-serve)과
[Tailscale 접근 정책](https://tailscale.com/docs/features/access-control/acls)을 참고한다.

## Bridge 설정

| Flag | 기본값·역할 |
| --- | --- |
| `-herdr-socket` | `$HOME/.config/herdr/herdr.sock`. 다른 named session은 명시적으로 지정 |
| `-claude-dir` | `$HOME/.claude`. Claude native transcript root |
| `-codex-dir` | `$HOME/.codex`. Codex home. `sessions/` 아래 rollout만 read-only로 읽는다 |
| `-receipts-dir` | `$HOME/.local/state/herdr-remote/receipts`. durable command receipts |
| `-listen` | `127.0.0.1:8787`. loopback만 허용 |
| `-static` | `web/dist`. 시작 시 `index.html` 존재 확인 |
| `-origins` | `http://127.0.0.1:8787`. comma-separated exact Origin 목록. 지정하면 기본값을 대체하므로 localhost 접속이 필요하면 `http://127.0.0.1:8787`도 직접 포함한다. Tailnet Origin은 host와 login이 모두 확정되면 자동 추가된다 |
| `-tailnet-host` | `auto`: `Self.DNSName`에서 끝의 점을 제거한 값. `off`: Tailnet 요청을 받지 않는 localhost 전용. 그 밖의 값은 Serve DNS host로 사용 |
| `-tailnet-login` | `auto`: 이 Tailscale node를 소유한 사용자 login. 명시 값은 Serve로 접근할 수 있는 단일 사용자 login |
| `-tailscale-bin` | Tailscale CLI 경로. 비우면 PATH, 이어서 알려진 설치 경로에서 찾는다 |
| `-project-root` | 없음. 반복 가능한 절대 경로. root가 Git repo면 root 하나, 아니면 바로 아래 한 단계의 Git repo가 새 session 후보다. hidden directory와 실제 경로가 root 밖인 symlink는 제외한다. 없으면 새 workspace는 만들 수 없고 열린 workspace에 새 tab만 연다 |

전체 flag는 `mise exec -- go run ./cmd/bridge -h`로 확인한다.
`-herdr-socket`은 CLI flag이며, 위 예시의 `HERDR_SOCKET_PATH`는 shell이 전달하는 값이다.

## 데이터와 lifecycle

```text
기존 Herdr agent → native transcript → Go Bridge → Chat
Chat command → durable receipt → Herdr binding 검증 → 기존 PTY
```

동작 규칙과 invariant는 [Architecture](docs/architecture.md#8-invariants)가 기준이다. 운영에서 알아 둘 것:

- `delivery_unknown`은 자동 재전송하지 않는다. 전달 여부는 Chat의 native transcript나 Terminal에서 확인한다.
- receipts directory를 지우면 오래된 command ID의 재수락을 막는 근거가 사라진다.
- WS 종료나 Bridge 재시작은 Herdr process를 중단하지 않는다.
- 새 session은 agent 인자 없이 pane의 사용자 shell에서 시작하므로 permission mode 등은 shell alias·설정을
  따른다. 처음 여는 폴더의 신뢰 확인 화면은 PC의 Herdr에서 수락한다.

## 운영과 알려진 제한

정적 파일 변경은 `pnpm --dir web build`, Go 변경은 binary 재빌드와 **Bridge만의 재시작**이
필요하다. Bridge 재시작은 Herdr와 agent process를 중단하지 않는다.

### Bridge 상시 실행

| 명령 | 동작 |
| --- | --- |
| `mise run build` | `web/dist`와 `bin/herdr-remote-bridge`를 build |
| `mise run install` | build 뒤 `$PREFIX/bin/herdr-remote-bridge`(기본 `~/.local`)에 설치. 직전 binary는 `.prev`로 남긴다 |
| `mise run redeploy` | vet·test 통과 뒤 install, LaunchAgent 재시작, health 확인. 응답이 없으면 `.prev`로 되돌리고 다시 재시작한다. 응답한 Bridge가 HEAD와 `web/dist`를 실행하지 않으면 실패로 끝낸다 |
| `mise run status` | HEAD, 설치된 binary, `web/dist`, 응답하는 Bridge의 revision과 client build를 한 줄씩 보인다. 실행 중인 Bridge가 HEAD·`web/dist`와 다르거나 응답이 없으면 1로 끝난다 |
| `mise run stop` | LaunchAgent를 내린다(`launchctl bootout`). 이미 내려가 있으면 안내만 한다. 내린 뒤 `redeploy`를 쓰려면 먼저 `start`한다 |
| `mise run start` | 내려간 LaunchAgent를 `~/Library/LaunchAgents/<label>.plist`(또는 `HERDR_REMOTE_LAUNCHD_PLIST`)로 올리고 Bridge를 시작해 health를 확인한다. 이미 올라가 있으면 실행만 보장한다 |
| `mise run restart` | `stop` 뒤 plist를 다시 올려 시작한다. `ProgramArguments`를 바꾼 뒤에는 `redeploy`의 `kickstart`로 반영되지 않으므로 이 task를 쓴다 |

`redeploy`·`stop`·`start`·`restart`는 macOS `launchd` 전용이며, `redeploy`는 LaunchAgent가 설치한 binary를
실행할 때만 진행한다. 다른 process manager는 `install` 뒤 직접 재시작한다. 되돌리는 것은 binary뿐이고 `web/dist`는 새
build로 남는다.

LaunchAgent는 [`contrib/launchd/herdr-remote.plist.example`](contrib/launchd/herdr-remote.plist.example)을
`~/Library/LaunchAgents/herdr-remote.plist`로 복사하고 `__HOME__`·`__REPO__`를 절대 경로로 바꾼 뒤
등록한다. `-project-root`, `-tailnet-host` 같은 flag는 `ProgramArguments`에 추가하고, 바꾼 뒤에는
`mise run restart`로 다시 올린다.

### 실행 중인 version 확인

release tag 없이 Git revision이 version이다. `go build`는 checkout의 `vcs.revision`·`vcs.modified`를 binary에
심고, web build는 bundle hash를 `web/dist/index.html`의 `herdr-build` meta와 service worker cache 이름에 함께
적는다. Bridge는 두 값을 `GET /api/sessions`의 `bridge`와 시작 로그에 보고한다.

- 세션 목록 제목 아래에 Bridge의 short revision이 보인다. `-dirty`는 untracked 파일을 포함해 변경이 있는 tree에서
  build했다는 뜻이고, `dev`는 `go run`으로 띄워 stamp가 없다는 뜻이다.
- 설치된 client build가 열려 있는 화면과 다르면 목록 위에 새로고침 안내가 뜬다.
- 터미널에서는 `mise run status`가 같은 값을 HEAD와 비교한다. 설치된 binary 파일과 실행 중인 process는 다를 수
  있으므로 실행 중인 값은 API 응답에서만 읽는다.

```sh
mkdir -p ~/.local/state/herdr-remote
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/herdr-remote.plist
```

label·설치 경로·health URL이 기본값과 다르면 gitignore된 `mise.local.toml`에 둔다.

```toml
[env]
HERDR_REMOTE_LAUNCHD_LABEL = "com.example.herdr-remote"
PREFIX = "{{env.HOME}}/opt/herdr-remote"
HERDR_REMOTE_HEALTH_URL = "http://127.0.0.1:8788/api/sessions"
```

`launchd`는 PATH가 최소한이고 macOS에서는 Tailscale CLI가 app bundle wrapper로만 있을 수
있으므로, 자동 탐색이 실패하면 `-tailscale-bin`으로 CLI 경로를 지정한다.

Herdr live handoff는 테스트에서 agent PID·native session을 보존했지만 terminal ID를
재발급했고 desktop client가 끊긴 동안 geometry를 기본 120×40으로 변경했다. 이 동작은
mobile Terminal의 resize와 별개다. 검증한 범위와 남은 조건은 [검증 기록](docs/records/verification.md)을 따른다.

### 진단 출력 공유

`doctor` JSON과 Bridge log에는 tailnet host, 소유자 login, socket·transcript 절대 경로가 들어갈 수
있다. issue 등에 붙일 때는 이 값을 `<tailnet-host>`, `<login>`, `<path>` 같은 placeholder로 바꾼다.
prompt와 transcript 본문은 log에 쓰지 않는다.

## 문서

| 문서 | 용도 |
| --- | --- |
| [AGENTS.md](AGENTS.md) | agent 작업 규칙과 제품 불변식 |
| [CONTRIBUTING.md](CONTRIBUTING.md) | 개발 server, 검증, issue·PR 정책 |
| [SECURITY.md](SECURITY.md) | 취약점 비공개 신고와 보안 범위 |
| [PRD](docs/prd.md) | 제품 요구와 범위 |
| [ADR](docs/adr.md) | architecture 결정 |
| [Architecture](docs/architecture.md) | 결정을 조합한 runtime 구조, data flow, invariant |
| [검증 기록](docs/records/verification.md) | 날짜별 실제 검증 결과 |
| [Integration 조사](docs/records/integration-findings.md) | Herdr/Claude/Codex 코드·runtime 근거 |
| [Herdr patch runbook](docs/herdr-patch.md) | 조건부 입력 patch의 확인·설치·rollback·upgrade |
| [Issues](https://github.com/psw7205/herdr-remote/issues) | 남은 작업과 완료 기준. label `P0`~`P2`는 우선순위, `in-use`·`blocked`·`optional`은 상태 |

## License

[Apache License 2.0](LICENSE)
