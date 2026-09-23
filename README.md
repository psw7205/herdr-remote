# Herdr Mobile Chat

Herdr가 이미 실행 중인 Claude Code session을 모바일 브라우저에서 읽고 이어서 대화하는
single-host client다. Herdr가 agent와 PTY를 소유한다. Chat은 native transcript의 투영이며
Bridge가 새 agent나 native session을 생성하지 않는다.

현재 구현은 Claude Code / macOS의 첫 handoff 경로다. Codex adapter와 PRD의 optional 기능은
[구현 계획](docs/implementation-plan.md)에 남아 있다.

## 실행 조건

- 이 저장소의 `mise.toml`: Go, Node.js, pnpm
- `herdr` repo의 `codex/mobile-binding` branch에서 빌드한 Herdr server
- 기존 Herdr pane에서 실행 중인 Claude Code와 접근 가능한 native transcript
- mobile 접근 시 Tailscale Serve와 Bridge의 소유자 identity 검증

기본 Herdr `0.9.1`에는 `agent.binding`/`agent.bound_input`가 없다. Bridge는 그 API가
없을 때 prompt를 다른 입력 API로 우회하지 않는다. Herdr patch의 `just ci`와 release
build가 통과했고, live handoff에서 기존 두 Claude PID와 native session ID를 유지했다.
Herdr 자체를 재시작하거나 updater가 기본 binary로 교체하면 patch를 다시 적용해야 한다.

## 개발과 실행

```sh
mise install
pnpm --dir web install --frozen-lockfile
mise run test
pnpm --dir web build
go run ./cmd/bridge -herdr-socket "$HERDR_SOCKET_PATH"
```

`HERDR_SOCKET_PATH`는 `herdr status server`가 출력한 기존 socket 경로를 사용한다.
Bridge는 기본적으로 `127.0.0.1:8787`에만 bind하고 `web/dist`를 제공한다.
브라우저에서 `http://127.0.0.1:8787`로 접속한다. server가 꺼져 있어도 agent는 계속 실행된다.
Bridge 재시작 후 transcript에서 history를 재구성하고 cursor epoch를 교체한다.

모바일 Tailnet HTTPS에서는 Bridge를 다음처럼 실행한다. `<tailnet-host>`는
`tailscale status --json`의 `Self.DNSName` 값이며 끝의 점을 제거한 host다.

```sh
go run ./cmd/bridge -herdr-socket "$HERDR_SOCKET_PATH" \
  -origins "http://127.0.0.1:8787,https://<tailnet-host>" \
  -tailnet-host "<tailnet-host>" -tailnet-login "<tailscale-login>"
tailscale serve --bg 8787
```

Serve는 Tailnet 내부에서만 사용하고 Funnel은 사용하지 않는다. `<tailscale-login>`은
`tailscale whoami`의 현재 사용자 로그인이다. Bridge는 Serve가 추가하는
`Tailscale-User-Login`을 정확히 대조한다. header가 없거나 다른 사용자면 읽기와 제어를
모두 거부한다. Serve는 클라이언트가 보낸 동일 header를 제거한다.
Tailnet ACL이 넓을 수 있으므로 identity 검증이
필수다. 추가로 [Tailscale ACL 문서](https://tailscale.com/docs/features/access-control/acls)에
따라 대상 mobile device만 허용하도록 network 정책을 좁히는 것을 권장한다.
정확한 Origin 값은 Bridge가 HTTP mutation과 WebSocket handshake 모두에서 검증한다.
비밀번호/OAuth/JWT는 추가하지 않았다.

## 동작과 확인

`GET /api/sessions`에서 실행 중인 agent를 찾는다. session별 `agent.binding`을 검증해
native session ID로 transcript를 찾고, JSONL을 incremental하게 읽는다. partial record는
완성되기 전까지 표시하지 않는다. 같은 cwd에 여러 session이 있으면 ID로 분리한다.

`POST /api/sessions/{id}/commands`는 `command_id`를 disk receipt에 먼저 기록한 뒤
Herdr의 조건부 입력 API를 한 번 호출한다. 같은 ID의 retry는 저장된 결과를 반환하고,
timeout/crash 뒤 전달이 불확실한 command는 자동 재전송하지 않는다. `accepted`는 PTY
전달 결과다. Chat 메시지는 native transcript에서 관찰된 뒤 표시된다.

`GET /api/sessions/{id}`의 snapshot cursor와
`WS /api/sessions/{id}/events?epoch=…&sequence=…`는 replay와 live 구독을 하나의 lock에서
연결한다. buffer miss나 Bridge restart 후에는 새로운 snapshot을 보낸다. WebSocket 종료는
Herdr pane이나 agent process를 종료하지 않는다.

Terminal은 같은 pane의 `pane.read` visible ANSI frame을 xterm.js에 교체 표시한다.
모바일 viewport로 Herdr PTY를 resize하지 않는다. Chat에서 표현하지 못하는 CLI interaction은
Terminal의 조건부 raw input과 특수 키로 처리한다. Terminal frame은 현재 화면이며
이전 frame 전체의 byte replay는 제공하지 않는다.

검증 명령:

```sh
mise run test
mise run vet
pnpm --dir web test
pnpm --dir web build
go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
```

`doctor`는 read-only다. 응답/terminal 내용이나 binding token을 출력하지 않고,
agent별 verified native binding과 visible terminal snapshot 가능 여부를 보고한다.

2026-09-23에 실행 중인 Herdr `0.9.1` 두 Claude session으로 확인한 결과:

- 두 native transcript가 다른 session ID로 Chat에 표시됨
- mobile prompt 한 건의 user/assistant record가 동일 native transcript와 desktop pane에 나타남
- 전후 Claude PID와 native session ID가 동일함
- 같은 `command_id` retry가 prompt를 다시 실행하지 않음
- 오래된 binding 요청이 Bridge와 Herdr에서 거부됨
- Bridge 종료 중에도 Claude PID가 유지되고, 재시작 후 다른 cursor epoch와 전체 대화 복구
- 모바일 Chat/Terminal 화면 전환 전후 Herdr pane geometry 동일

Herdr live handoff 자체는 당시 desktop client 연결을 끊으며 geometry를 기본 120×40으로
바꿨다. 이는 mobile Terminal의 resize가 아니며, desktop Herdr client 재접속 시 기존
client의 크기 소유권이 다시 적용된다. 최초 handoff 전에 desktop geometry를 유지해야 한다면
현재 live handoff 구현을 별도로 개선해야 한다.

설계 근거는 [PRD](docs/prd.md), [ADR](docs/adr.md),
[실제 integration 조사](docs/integration-findings.md)를 따른다.
