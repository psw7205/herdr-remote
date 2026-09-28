# Herdr Mobile Chat — Architecture

**Related:** [PRD](prd.md), [ADR](adr.md), [Backlog](backlog.md), [Herdr patch runbook](herdr-patch.md)

## 1. 개요

ADR의 결정을 조합해 현재 구현된 runtime 구조, data flow, invariant를 정리한다. Bridge는
Herdr에서 이미 실행 중인 Claude Code session을 발견하고, native transcript를 읽어 Chat으로
projection하며, 입력은 Herdr가 현재 `runtime_binding`을 검증하는 `agent.bound_input`으로만
전달한다. Bridge는 agent를 start/resume하지 않고 conversation을 저장하지 않는다. 결정의 근거는
각 ADR을, 검증 범위는 [검증 기록](records/verification.md)을, 남은 작업은 [Backlog](backlog.md)를 따른다.

## 2. Runtime 구성도

```text
┌──────────── Phone (PWA) ─────────────┐   ┌──── Bridge host의 browser ────┐
│ Sessions · Chat · Terminal(xterm.js) │   │ http://127.0.0.1:8787         │
└──────────────────┬───────────────────┘   └───────────────┬───────────────┘
                   │ HTTPS / WSS (tailnet)                 │ loopback HTTP / WS
┌──────────────────▼───────────────────┐                   │
│ Tailscale Serve                      │                   │
│ https://<tailnet-host>               │                   │
│ Tailscale-User-Login, X-Forwarded-*  │                   │
└──────────────────┬───────────────────┘                   │
                   │ proxy → 127.0.0.1:8787                │
┌──────────────────▼───────────────────────────────────────▼───────────────┐
│ Bridge (cmd/bridge, 단일 Go process, loopback bind)                      │
│                                                                          │
│  httpapi ── Host / identity / Origin gate ── static web/dist             │
│     │            │                  │                                    │
│     │ commands   │ events (WS)      │ terminal (GET poll)                │
│     ▼            ▼                  ▼                                    │
│  command.Store  stream.Session   session.Registry ── 2s Refresh          │
│  (receipt 파일) (epoch·ring)      │        │                             │
│                     ▲             │        └── transcript.Watch          │
│                     └─ projection ┘            (fsnotify + 1s poll)      │
│                        (internal/claude)                                 │
│                                   │                                      │
│                              herdr.Gateway                               │
└───────────────────────────────────┬──────────────────────┬───────────────┘
                   Herdr JSON socket │                      │ read-only file
┌───────────────────────────────────▼──────────────────────▼───────────────┐
│ Herdr host                                                               │
│  Herdr server (mobile-binding patch: agent.binding, agent.bound_input)   │
│   └ Workspace └ Tab └ Pane └ PTY └ Claude Code ──→ native JSONL          │
└──────────────────────────────────────────────────────────────────────────┘
```

`cmd/doctor`는 같은 Herdr socket과 Tailscale CLI를 read-only로 조회하는 별도 진단 binary다.

## 3. Code 구성

| 경로 | 책임 |
| --- | --- |
| `cmd/bridge/` | flag 해석, loopback bind 검사, receipt store·registry·HTTP server 조립, 2s Herdr refresh loop |
| `cmd/doctor/` | Herdr capability(`conditional_input`, `verified_binding`)와 `tailnet` 설정의 read-only 진단 |
| `internal/herdr/` | Herdr JSON socket client. `session.snapshot`, `agent.binding`, `agent.bound_input`, `pane.read`, `pane.layout`, `pane.process_info`(doctor 전용). 미지원 method는 `ErrUnsupported` |
| `internal/session/` | agent discovery, binding 검증, item identity·lifecycle, transcript watcher 수명, bound input·Terminal 전 binding 재확인 |
| `internal/claude/` | native transcript 경로 resolve, JSONL record decode, parent chain projection |
| `internal/transcript/` | offset 기반 incremental read(`Tailer`), `fsnotify` + polling reconciliation(`Watch`) |
| `internal/command/` | `command_id`별 durable receipt. conversation 내용은 저장하지 않는다 |
| `internal/stream/` | session별 snapshot·replay ring·live 구독의 원자적 경계, epoch/sequence |
| `internal/httpapi/` | HTTP routes, WebSocket, Host·Origin·Tailscale identity 경계, security header |
| `internal/tailnet/` | `tailscale status --json`, `tailscale serve status --json` 조회와 host·owner login 자동 감지 |
| `web/src/` | thin client: session 목록, Chat(Markdown), Composer, xterm.js Terminal, command 재시도 상태 |
| `web/public/` | PWA manifest, icon, service worker |

## 4. Data flow

### 4.1 대화 읽기

```text
Bridge 2s tick → Registry.Refresh
  → Herdr session.snapshot (agent 목록)
  → agent == claude인 pane마다 agent.binding
      검증 조건: agent claude, terminal_id 일치, token·process_id·native_session_id 존재
  → claude.Resolve: <claude-dir>/projects/*/<native-id>.jsonl 이 정확히 하나일 때만 Chat 허용
  → item.startWatch → transcript.Watch
      directory fsnotify 등록 후 초기 read, 이후 event 또는 1s tick마다 offset 이후만 read
      file identity 변경·truncate·head 불일치면 Batch.Reset
  → claude.Decode + Projection (최신 leaf의 parent chain 중 text가 있는 record)
  → stream.Session
      reset/첫 load 또는 앞부분 변경 → Reset: 새 epoch + `session.snapshot` event
      뒤에 추가만 됨 → Commit: `message.user` / `message.assistant` event, sequence++
  → WS 구독자
```

- 같은 native ID가 한 Refresh에서 두 pane에서 검증되면 어느 쪽도 binding과 함께
  `claude:<native-id>`를 차지하지 않는다([ADR-034](adr.md#adr-034--herdr가-runtime-binding을-검증한-command만-전달한다)).
- transcript decode·projection 실패는 item을 invalid로 표시하고 `session.error`를 보낸다.
  invalid 동안 목록과 snapshot의 `chat`은 `false`이고 prompt는 `CHAT_UNAVAILABLE`로 거부된다.
- 근거: [ADR-002](adr.md#adr-002--chat은-새로운-conversation이-아니라-projection이다),
  [ADR-020](adr.md#adr-020--transcript-watcher는-incremental-parsing을-사용한다),
  [ADR-021](adr.md#adr-021--full-transcript는-필요할-때-snapshot으로-재구성한다).

### 4.2 Command 전송

```text
Composer / Stop / Terminal key
  → POST /api/sessions/{id}/commands
      { command_id, session_id, runtime_binding, command_type, payload.text }
  → httpapi: exact Origin, application/json, 72 KiB body, unknown field 거부,
             command_type ∈ prompt | interrupt | terminal_input, text ≤ 65536 byte
  → command.Store.Execute
      reserve: UUIDv4 검사, <command_id>.json O_EXCL 생성 + fsync(file, dir),
               초기 상태 delivery_unknown, 요청 전체의 sha256 digest 저장
      기존 receipt: digest 같음 → 저장된 결과 반환(재dispatch 없음)
                    digest 다름 → rejected COMMAND_ID_CONFLICT
  → Registry.BoundInput 사전 검사
      active, runtime_binding 일치, prompt는 chat 가능·status idle|completed,
      terminal_input은 terminal 가능
  → Herdr agent.bound_input { target, binding, input{type, text} }
  → 결과 mapping 후 finish: temp file → rename → fsync(dir)
      accepted          → 202
      rejected + code   → 409 (RUNTIME_BINDING_MISMATCH, SESSION_ENDED, HERDR_UNSUPPORTED, SESSION_CHANGED …)
      delivery_unknown  → 202 (Herdr delivery_unknown, 분류 불가 오류, receipt 읽기·완료 기록 실패)
```

- 응답은 전달 결과일 뿐이다. agent 응답은 transcript를 거쳐 4.1 경로로만 도착한다
  ([ADR-004](adr.md#adr-004--read-path와-write-path를-분리한다)).
- browser는 prompt의 `command_id`를 `sessionStorage`에 보존해 reload 뒤 재시도에도 같은 ID를 쓴다.
  `delivery_unknown`은 자동 재전송하지 않는다. interrupt와 `terminal_input`은 매번 새 UUID다.
- 근거: [ADR-023](adr.md#adr-023--interrupt는-prompt와-별도-command로-모델링한다),
  [ADR-033](adr.md#adr-033--command-receipt로-retry-중복-실행을-방지한다),
  [ADR-034](adr.md#adr-034--herdr가-runtime-binding을-검증한-command만-전달한다),
  [ADR-036](adr.md#adr-036--herdr-patch는-소유-fork에서-유지한다).

### 4.3 재연결 (epoch / sequence / replay / snapshot)

```text
client connect
  → GET /api/sessions/{id}            snapshot { cursor{epoch, sequence}, data: Message[] }
  → WS /api/sessions/{id}/events?epoch=E&sequence=N
  → stream.Session.Subscribe (Commit·Snapshot과 같은 lock)
      epoch 같음 && ring이 N+1부터 보유 → ring에서 N 이후 event replay
      그 밖(epoch 변경, ring miss, cursor가 앞섬) → `session.snapshot` event 1개
  → 같은 channel로 live event
client
  - 같은 epoch의 sequence ≤ last는 무시, gap이나 epoch 불일치면 WS를 닫고 재연결
  - close 뒤 1.2s, 연결 실패 뒤 2s에 재연결. foreground 복귀(visibilitychange) 시 스스로 재연결
```

- epoch는 `stream.New`(Bridge 시작, 새 item)와 `Reset`(transcript 재동기화, 비활성 item 복귀)에서
  새로 발급한다. Bridge restart는 항상 새 epoch이므로 client는 snapshot으로 복구한다.
- replay ring은 session당 최근 1024 event 또는 payload 8 MiB까지다. 느린 구독자(buffer 128 초과)는
  channel이 닫혀 재연결한다.
- WS 종료는 구독 해제만 수행한다. agent, pane, Herdr session에는 영향이 없다.
- 근거: [ADR-008](adr.md#adr-008--snapshot--live-event-모델을-사용한다),
  [ADR-009](adr.md#adr-009--websocket은-transport일-뿐-session이-아니다),
  [ADR-019](adr.md#adr-019--local-event-buffer는-제한적으로-유지한다),
  [ADR-029](adr.md#adr-029--server-restart를-agent-session-failure로-취급하지-않는다).

### 4.4 Terminal

```text
TerminalView, visible일 때 1s 간격 poll
  → GET /api/sessions/{id}/terminal   header X-Runtime-Binding
  → Registry.Terminal
      meta의 binding 일치 확인 → Herdr agent.binding 재확인
      → pane.read { source: visible, format: ansi, strip_ansi: false }
      → pane.layout의 pane rect(zoomed면 tab area) → cols/rows render hint (500ms timeout, 실패 시 생략)
      → agent.binding 다시 확인 후 frame 반환. 불일치·실패는 409 SESSION_CHANGED
  → xterm.js: term.reset(); term.write(frame.text)
      local grid·font size만 조정, scrollback 0
key 입력 → command_type terminal_input (4.2 경로)
```

Herdr PTY resize, private attach, takeover/resume은 호출하지 않는다. frame은 교체 표시이며 중간
frame이나 raw output history를 보장하지 않는다
([ADR-035](adr.md#adr-035--mobile-terminal은-기존-pty의-passive-mirror다)).

## 5. HTTP/WS API

| Method·Path | 형태 | 역할 |
| --- | --- | --- |
| `GET /api/sessions` | JSON | `ended`·`superseded`가 아닌 item의 `Meta` 목록과 `herdr.conditional_input`(`supported`/`unsupported`/`unknown`). web은 3s poll |
| `GET /api/sessions/{id}` | JSON | `session`(`Meta`)과 `snapshot`(`cursor`, `data`). Chat 화면은 metadata를 3s poll |
| `GET /api/sessions/{id}/events` | WebSocket | query `epoch`, `sequence`. replay 또는 `session.snapshot` 뒤 live event. Origin 검사 |
| `GET /api/sessions/{id}/terminal` | JSON, 1s poll | `X-Runtime-Binding` header 필수. visible ANSI frame과 `cols`/`rows` |
| `POST /api/sessions/{id}/commands` | JSON | `prompt` / `interrupt` / `terminal_input`. Origin 검사. 결과 `accepted`/`rejected`/`delivery_unknown` |
| `/` | static | `-static` directory(`web/dist`). index 없는 directory listing 금지 |

WS event envelope은 `protocol_version`(1), `event_id`(`<epoch>:<sequence>`), `session_id`, `cursor`,
`timestamp`, `type`, `payload`, `source`다. 현재 type은 다음 다섯 가지다.

| Type | 출처 | Payload |
| --- | --- | --- |
| `session.snapshot` | `bridge` | `cursor`, `data`(Message 배열). Herdr socket method `session.snapshot`과 이름만 같다 |
| `message.user`, `message.assistant` | `claude.transcript` | `id`, `role`, `text`, `timestamp` |
| `agent.status` | `bridge` | `status`, `lifecycle`, 필요 시 `successor_id` |
| `session.error` | `bridge` | `message` (Terminal 전환 안내) |

`Meta`는 `id`, `agent`, `pane_id`, `project`, `title`, `status`, `runtime_binding`, `chat`, `terminal`,
`active`, `lifecycle`, `successor_id`, `last_activity`, `last_message`를 가진다. client는 native
transcript 형식을 모른다([ADR-007](adr.md#adr-007--unified-event-protocol을-client-contract로-사용한다),
[ADR-027](adr.md#adr-027--resthttp--websocket-hybrid-transport를-사용한다)).

## 6. Session lifecycle 요약

- item ID는 검증된 native session이면 `claude:<native-id>`, 식별 전이면 `pane:<pane-id>`다.
- `lifecycle`: `active`, `unverified`, `unbound`, `superseded`(+`successor_id`), `ended`.
- `status`: Herdr `idle`/`done`/`working`/`blocked`를 `idle`/`completed`/`working`/`needs_attention`으로,
  그 밖은 `error`로 옮긴다.
- binding이 없는 상태에서는 prompt, interrupt, Terminal read/input이 모두 fail closed한다.
- transcript watcher는 `active`·`unverified` 동안만 돈다. `ended`·`superseded`가 되면 닫고, 다시
  `active`가 되면 처음부터 읽어 새 epoch으로 동기화한다.
- `GET /api/sessions`는 `ended`·`superseded` item을 빼지만 `GET /api/sessions/{id}`와 WS는 계속 제공한다.
  client는 3s 목록 poll에서 열린 item이 빠지면 `GET /api/sessions/{id}`의 `successor_id`를 읽고, 목록에 있는
  successor와 그 binding으로 Chat·Terminal을 전환한다.

continuity, supersession, `ended` 판정 규칙은
[ADR-034](adr.md#adr-034--herdr가-runtime-binding을-검증한-command만-전달한다)가 기준이며 여기서 반복하지 않는다.

## 7. 보안 경계

- `cmd/bridge`는 `-listen`이 `localhost`/`127.0.0.1`/`::1`이 아니면 종료한다(exit 2).
- `httpapi.Handler`는 모든 요청에 `nosniff`, `X-Frame-Options: DENY`, `frame-ancestors 'none'`을 붙이고
  `Host`를 loopback host 또는 확정된 tailnet host(`:443` 포함)로 제한한다.
- tailnet host, tailnet Origin, 또는 `X-Forwarded-*`·`Tailscale-User-*` header 중 하나라도 있으면
  tailnet 요청으로 본다. 이때 tailnet host·owner login이 확정돼 있고 `Tailscale-User-Login`이
  owner와 같으며 `Host`가 tailnet host여야 한다. 그래서 Serve 외 local reverse proxy는 지원하지 않는다.
- `POST .../commands`와 WS는 exact Origin allowlist를 검사한다. `-origins` 기본값에 더해 host와
  login이 모두 확정되면 `https://<tailnet-host>`를 추가한다. login 없는 tailnet Origin은 제외한다.
- Tailnet host·login은 `-tailnet-host`·`-tailnet-login`(기본 `auto`)으로 시작 시 한 번 해석하고,
  실패하면 localhost 전용으로 동작한다. Funnel과 wildcard CORS는 지원하지 않는다.

근거와 위협 모델은 [ADR-024](adr.md#adr-024--tailnet을-primary-security-boundary로-사용한다)를 따른다.
network 계층 ACL은 [Backlog P0-03](backlog.md#p0--배포와-핵심-안정성)이다.

## 8. Invariants

| ID | Invariant | 강제 위치 |
| --- | --- | --- |
| I1 | Chat이 가능한 item 하나는 Herdr가 검증한 기존 native session 하나다 | `internal/session` `claude:<native-id>` identity, `internal/claude` `Resolve`의 단일 경로 조건 |
| I2 | Mobile 입력은 agent process를 만들지 않는다 | `internal/herdr`에 start/resume 경로 없음. 입력은 `agent.bound_input`만 |
| I3 | Transcript는 read-only다 | `internal/transcript`는 `os.Open`만 사용. Bridge의 disk write는 `internal/command` receipt뿐 |
| I4 | browser/WS 종료는 agent, pane, session에 영향을 주지 않는다 | `internal/httpapi` WS 종료 시 구독 해제만 수행 |
| I5 | Agent-specific parsing은 Bridge 안에만 있다 | `internal/claude` |
| I6 | Client는 native transcript format을 모른다 | `internal/session` `Message{id, role, text, timestamp}`와 `internal/stream` envelope |
| I7 | Chat이 표현하지 못하거나 transcript가 불명확하면 같은 pane의 Terminal로 fallback한다 | `session.error`, `chat: false`, `internal/session` `Registry.Terminal` |
| I8 | Bridge restart 뒤 Herdr와 native transcript에서 상태를 재구성한다 | `cmd/bridge` 시작 Refresh, 새 epoch snapshot, `-receipts-dir` receipt 재사용 |
| I9 | Bridge는 conversation의 유일한 copy가 아니다 | stream state는 memory projection. receipt는 digest와 결과만 저장 |
| I10 | Bridge 접근은 localhost 또는 Tailscale Serve 경유만 허용한다 | `cmd/bridge` loopback bind 검사, `internal/httpapi` Host·identity·Origin gate |
| I11 | 모든 write는 현재 `runtime_binding`을 Herdr가 검증한 경로로만 전달한다 | `internal/session` `BoundInput` 사전 검사, Herdr `agent.bound_input` |
| I12 | 같은 `command_id`는 한 번만 dispatch하고 `delivery_unknown`을 자동 재전송하지 않는다 | `internal/command` `Store.Execute`, `web/src` `commandDelivery` |
| I13 | Terminal은 Herdr PTY 크기를 바꾸지 않는다 | `internal/herdr`는 read-only `pane.read`·`pane.layout`만 호출 |

실제 검증 범위와 미검증 scenario는 [검증 기록](records/verification.md)을 따른다.

## 9. 기술 선택

| 영역 | 결정 |
| --- | --- |
| Bridge | Go `net/http`(method pattern routing), `coder/websocket`, `fsnotify`, `log/slog`. 단일 process([ADR-010](adr.md#adr-010--bridge는-single-process-control-plane으로-시작한다)) |
| Client | React + TypeScript + Vite, pnpm. Terminal은 `@xterm/xterm` |
| Toolchain | `mise.toml` |
| Persistence | `command_id`당 JSON receipt 파일 하나(`O_EXCL` reserve, fsync, temp+rename finish). conversation DB·Redis·broker 없음 |
| Transport | semantic event는 WebSocket, Terminal frame은 HTTP polling으로 물리적으로 분리([ADR-028](adr.md#adr-028--terminal-stream과-semantic-event-stream을-논리적으로-분리한다)) |
| Transcript watch | directory `fsnotify` + 1s polling reconciliation, offset 이후 incremental read |

receipt 보존 기간과 buffer 운영은 [Backlog P2-06](backlog.md#p2--필요가-확인될-때-추가할-기능), 장기 실행
resource 상한은 [Backlog P1-13](backlog.md#p1--핵심-ux와-다음-agent)에서 다룬다.

## 10. 미구현 경계

| 영역 | 현재 | Backlog |
| --- | --- | --- |
| Codex | `Registry.Refresh`는 `claude` agent만 관찰한다. binding과 transcript adapter 없음 | [P1-01, P1-02](backlog.md#p1--핵심-ux와-다음-agent) |
| Tool activity card | transcript의 tool record는 Chat에 표시하지 않는다 | [P2-01](backlog.md#p2--필요가-확인될-때-추가할-기능) |
| Structured permission/question | Terminal fallback만 있다 | [P2-02](backlog.md#p2--필요가-확인될-때-추가할-기능) |
| Changed Files·diff | route와 Git 조회 없음 | [P2-03](backlog.md#p2--필요가-확인될-때-추가할-기능) |
| Attachment | route와 host file 처리 없음 | [P2-04](backlog.md#p2--필요가-확인될-때-추가할-기능) |
| Notification | Web Push 없음 | [P2-05](backlog.md#p2--필요가-확인될-때-추가할-기능) |
| Streaming delta | 완성된 JSONL text record만 전송 | [P1-07](backlog.md#p1--핵심-ux와-다음-agent) |
| transcript 이동 | 발견 시점 경로를 계속 watch | [P1-12](backlog.md#p1--핵심-ux와-다음-agent) |

## Decision Priority

구현 중 선택이 충돌하면 다음 순서로 판단한다.

```text
1. Existing Herdr session integrity
2. Correctness
3. Recoverability
4. Mobile usability
5. Simplicity
6. Rich UI
7. Feature count
```

즉 richer Chat UI 때문에 기존 terminal session을 불안정하게 만드는 선택은 하지 않는다.

> **Do not recreate the agent session; project the existing session into a mobile-friendly interface.**
