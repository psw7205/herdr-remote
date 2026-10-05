# Herdr Mobile Chat — Architecture Decision Records

**Related:** [prd.md](prd.md), [architecture.md](architecture.md)

각 ADR의 상태는 해당 ADR의 `Status`를 따른다. 결정을 조합한 전체 구조는 [architecture.md](architecture.md)에 있다.
`runtime_binding`, epoch, fail closed 같은 용어는 [용어](architecture.md#용어)를 따른다.

| ID | 제목 | Status |
| --- | --- | --- |
| [ADR-001](#adr-001--herdr를-runtime-owner로-유지한다) | ADR-001 — Herdr를 Runtime Owner로 유지한다 | Accepted |
| [ADR-002](#adr-002--chat은-새로운-conversation이-아니라-projection이다) | ADR-002 — Chat은 새로운 Conversation이 아니라 Projection이다 | Accepted |
| [ADR-003](#adr-003--native-agent-session-id를-conversation-identity로-사용한다) | ADR-003 — Native Agent Session ID를 Conversation Identity로 사용한다 | Accepted |
| [ADR-004](#adr-004--read-path와-write-path를-분리한다) | ADR-004 — Read Path와 Write Path를 분리한다 | Accepted |
| [ADR-005](#adr-005--pty를-interactive-truth로-유지한다) | ADR-005 — PTY를 Interactive Truth로 유지한다 | Accepted |
| [ADR-006](#adr-006--agent-adapter-boundary를-둔다) | ADR-006 — Agent Adapter Boundary를 둔다 | Accepted |
| [ADR-007](#adr-007--unified-event-protocol을-client-contract로-사용한다) | ADR-007 — Unified Event Protocol을 Client Contract로 사용한다 | Accepted |
| [ADR-008](#adr-008--snapshot--live-event-모델을-사용한다) | ADR-008 — Snapshot + Live Event 모델을 사용한다 | Accepted |
| [ADR-009](#adr-009--websocket은-transport일-뿐-session이-아니다) | ADR-009 — WebSocket은 Transport일 뿐 Session이 아니다 | Accepted |
| [ADR-010](#adr-010--bridge는-single-process-control-plane으로-시작한다) | ADR-010 — Bridge는 Single Process Control Plane으로 시작한다 | Accepted |
| [ADR-011](#adr-011--client는-thin-client로-유지한다) | ADR-011 — Client는 Thin Client로 유지한다 | Accepted |
| [ADR-012](#adr-012--webpwa를-첫-client로-사용한다) | ADR-012 — Web/PWA를 첫 Client로 사용한다 | Accepted |
| [ADR-013](#adr-013--terminal은-chat과-동일-session의-secondary-view다) | ADR-013 — Terminal은 Chat과 동일 Session의 Secondary View다 | Accepted |
| [ADR-014](#adr-014--file-editing은-제공하지-않고-review까지만-지원한다) | ADR-014 — File Editing은 제공하지 않고 Review까지만 지원한다 | Accepted |
| [ADR-015](#adr-015--attachment는-host-file로-변환한-뒤-existing-agent에-전달한다) | ADR-015 — Attachment는 Host File로 변환한 뒤 Existing Agent에 전달한다 | Proposed |
| [ADR-016](#adr-016--changed-files는-git을-read-only-source로-사용한다) | ADR-016 — Changed Files는 Git을 Read-only Source로 사용한다 | Accepted |
| [ADR-017](#adr-017--push-notification은-derived-event로-취급한다) | ADR-017 — Push Notification은 Derived Event로 취급한다 | Proposed |
| [ADR-018](#adr-018--attention-state는-domain-level-derived-state로-관리한다) | ADR-018 — Attention State는 Domain-level Derived State로 관리한다 | Accepted |
| [ADR-019](#adr-019--local-event-buffer는-제한적으로-유지한다) | ADR-019 — Local Event Buffer는 제한적으로 유지한다 | Accepted |
| [ADR-020](#adr-020--transcript-watcher는-incremental-parsing을-사용한다) | ADR-020 — Transcript Watcher는 Incremental Parsing을 사용한다 | Accepted |
| [ADR-021](#adr-021--full-transcript는-필요할-때-snapshot으로-재구성한다) | ADR-021 — Full Transcript는 필요할 때 Snapshot으로 재구성한다 | Accepted |
| [ADR-022](#adr-022--structured-actions에는-capability-negotiation을-사용한다) | ADR-022 — Structured Actions에는 Capability Negotiation을 사용한다 | Accepted |
| [ADR-023](#adr-023--interrupt는-prompt와-별도-command로-모델링한다) | ADR-023 — Interrupt는 Prompt와 별도 Command로 모델링한다 | Accepted |
| [ADR-024](#adr-024--tailnet을-primary-security-boundary로-사용한다) | ADR-024 — Tailnet을 Primary Security Boundary로 사용한다 | Accepted |
| [ADR-025](#adr-025--host는-mvp에서-하나만-지원한다) | ADR-025 — Host는 MVP에서 하나만 지원한다 | Accepted |
| [ADR-026](#adr-026--bridge-api는-ui-프레임워크에-독립적이어야-한다) | ADR-026 — Bridge API는 UI 프레임워크에 독립적이어야 한다 | Accepted |
| [ADR-027](#adr-027--resthttp--websocket-hybrid-transport를-사용한다) | ADR-027 — REST/HTTP + WebSocket Hybrid Transport를 사용한다 | Accepted |
| [ADR-028](#adr-028--terminal-stream과-semantic-event-stream을-논리적으로-분리한다) | ADR-028 — Terminal Stream과 Semantic Event Stream을 논리적으로 분리한다 | Accepted |
| [ADR-029](#adr-029--server-restart를-agent-session-failure로-취급하지-않는다) | ADR-029 — Server Restart를 Agent Session Failure로 취급하지 않는다 | Accepted |
| [ADR-030](#adr-030--graceful-degradation을-핵심-compatibility-strategy로-사용한다) | ADR-030 — Graceful Degradation을 핵심 Compatibility Strategy로 사용한다 | Accepted |
| [ADR-031](#adr-031--mvp-구현-순서는-vertical-slice를-따른다) | ADR-031 — MVP 구현 순서는 Vertical Slice를 따른다 | Completed |
| [ADR-032](#adr-032--mvp-완료-기준은-architecture가-아니라-실제-handoff-flow다) | ADR-032 — MVP 완료 기준은 Architecture가 아니라 실제 Handoff Flow다 | Completed |
| [ADR-033](#adr-033--command-receipt로-retry-중복-실행을-방지한다) | ADR-033 — Command receipt로 retry 중복 실행을 방지한다 | Accepted |
| [ADR-034](#adr-034--herdr가-runtime-binding을-검증한-command만-전달한다) | ADR-034 — Herdr가 runtime binding을 검증한 command만 전달한다 | Accepted |
| [ADR-035](#adr-035--mobile-terminal은-기존-pty의-passive-mirror다) | ADR-035 — Mobile Terminal은 기존 PTY의 passive mirror다 | Accepted |
| [ADR-036](#adr-036--herdr-patch는-소유-fork에서-유지한다) | ADR-036 — Herdr patch는 소유 fork에서 유지한다 | Accepted |
| [ADR-037](#adr-037--새-session-생성은-herdr에-요청한다) | ADR-037 — 새 session 생성은 Herdr에 요청한다 | Accepted |
| [ADR-038](#adr-038--codex-chat은-herdr가-보고한-native-session으로-read-only-표시한다) | ADR-038 — Codex Chat은 Herdr가 보고한 native session으로 read-only 표시한다 | Accepted |
| [ADR-039](#adr-039--tool-호출은-chat-item으로-투영하고-결과는-message-갱신으로-보낸다) | ADR-039 — Tool 호출은 Chat item으로 투영하고 결과는 message 갱신으로 보낸다 | Accepted |

---

## ADR-001 — Herdr를 Runtime Owner로 유지한다

### Status

Accepted

### Context

Herdr는 이미 다음 책임을 수행한다.

* workspace 관리
* tab / pane 관리
* PTY 관리
* Claude Code / Codex process 실행
* agent 상태 추적
* native agent session 연결
* terminal interaction

모바일 채팅 기능을 구현하면서 별도의 agent runner를 추가하면 동일 agent에 대해 두 개의 lifecycle owner가 생길 수 있다.

예:

```text
Herdr
 └─ Codex process

Mobile backend
 └─ Codex process
```

이 경우 다음 문제가 발생한다.

* session 중복
* context 분리
* process lifecycle 충돌
* worktree / cwd 불일치
* terminal과 mobile conversation divergence
* agent version / config 차이

### Decision

Herdr를 유일한 agent runtime owner로 유지한다.

Herdr Mobile Chat은 agent를 직접 실행하거나 기존 native session을 resume하지 않는다.
사용자가 명시적으로 새 session을 요청하면 Herdr 공개 API로 생성을 요청할 수 있고, 그 범위는
[ADR-037](#adr-037--새-session-생성은-herdr에-요청한다)을 따른다.

```text
Herdr
  │
  └── Agent Process
        │
        ├── Terminal View
        └── Mobile Chat Projection
```

모바일에서 보내는 입력도 기존 Herdr pane을 통해 실행 중 process로 전달한다.

### Consequences

#### Positive

* 기존 Herdr workflow 유지
* PC ↔ mobile seamless handoff
* duplicate process 방지
* agent config / env / cwd 일관성 유지
* runtime 구현 중복 제거

#### Negative

* Herdr가 실행 중이지 않으면 앱이 독립적으로 agent를 실행할 수 없음
* Herdr API capability에 일부 기능이 종속됨
* 모바일에서 완전 독립적인 agent lifecycle을 제공할 수 없음

### Rejected Alternatives

#### 자체 agent runner

Mobile backend가 Claude/Codex를 직접 실행한다.

거부 이유:

Herdr와 책임이 중복된다.

#### Herdr session을 모바일 접속 시 resume

모바일에서 기존 native session ID를 이용해 새로운 process를 생성한다.

거부 이유:

동일 conversation에 두 process가 접근하는 상황이 발생할 수 있다.

---

## ADR-002 — Chat은 새로운 Conversation이 아니라 Projection이다

### Status

Accepted

### Context

실행 중인 Claude/Codex는 이미 자체 conversation state와 transcript를 가지고 있다.

모바일용 DB에 별도의 conversation을 생성하면 다음과 같은 두 개의 history가 존재하게 된다.

```text
Agent Transcript

vs

Mobile Conversation DB
```

두 데이터를 완벽하게 동기화하기 어렵다.

### Decision

Mobile Chat은 기존 native agent session의 **projection/read model**로 정의한다.

```text
Native Agent Session
        │
        ├── Native Transcript
        │
        ▼
Agent Adapter
        │
        ▼
Unified Events
        │
        ▼
Chat View
```

Mobile backend는 conversation 자체를 소유하지 않는다.

### Source of Truth

| Data                   | Source                  |
| ---------------------- | ----------------------- |
| Agent process          | Herdr                   |
| Agent session identity | Herdr                   |
| Conversation           | Claude/Codex transcript |
| Terminal state         | PTY                     |
| Mobile unread state    | Mobile backend (미구현) |
| Client preferences     | Mobile backend (미구현) |

### Consequences

#### Positive

* PC와 모바일 history가 자연스럽게 동일
* duplicate persistence 제거
* mobile backend 장애가 conversation 유실로 이어지지 않음
* transcript migration 불필요

#### Negative

* agent transcript format에 의존
* transcript format 변경에 adapter 유지보수 필요
* transcript에 없는 interactive state는 Chat에서 표현할 수 없음

---

## ADR-003 — Native Agent Session ID를 Conversation Identity로 사용한다

### Status

Accepted

### Context

Transcript를 찾는 방법으로 다음 후보가 있다.

* cwd
* project directory
* latest modified transcript
* process ID
* pane ID
* native agent session ID

cwd 기반 추측은 여러 session이 같은 repository에서 실행되는 경우 잘못된 transcript를 선택할 수 있다.

### Decision

가능한 경우 native agent session ID를 conversation의 canonical identity로 사용한다.

내부 SessionRef는 개념적으로 다음 정보를 가진다.

```text
SessionRef

host_id
workspace_id
tab_id
pane_id

agent_type
agent_session_id
```

`agent_session_id`가 transcript 식별의 primary key다.

Herdr pane identity는 runtime target을 찾는 데 사용한다.

### Fallback

native session ID가 없는 agent에 대해서만 제한적으로 fallback discovery를 허용한다.

예:

```text
agent type
+ cwd
+ process start time
+ transcript mtime
```

첫 slice에서는 cwd/mtime guessing을 사용하지 않는다.
실제 PID metadata와 OS process 시작 시간을 대조한 read-only resolution은 가능하지만
write authority는 Herdr가 검증한 binding만 사용한다.
binding을 잃은 기존 session을 같은 pane에 유지할 때 쓰는 `pane_id`, Herdr `agent_session`,
걸러진 `agent.binding` 결과의 native ID도 read-only continuity 신호일 뿐 write authority가 아니다(ADR-034).
fallback 결과가 ambiguous하면 임의 선택하지 않는다.

사용자에게:

```text
Conversation could not be identified.
Open Terminal
```

을 제공한다.

### Consequences

잘못된 transcript를 다른 pane에 연결하는 위험을 최소화한다.

---

## ADR-004 — Read Path와 Write Path를 분리한다

### Status

Accepted

### Context

Chat UI를 만들기 위해서는 두 가지 별도 문제가 있다.

1. 기존 대화를 어떻게 읽을 것인가
2. 사용자의 새로운 입력을 어떻게 agent에 전달할 것인가

Transcript는 읽기에 적합하지만 직접 수정해서는 안 된다.

PTY는 쓰기에 적합하지만 terminal output을 conversation source로 사용하기에는 불안정하다.

### Decision

다음 구조를 사용한다.

#### Read path

```text
Native Transcript
       ↓
Agent Adapter
       ↓
Normalized Events
       ↓
Chat
```

#### Write path

```text
Chat Composer
      ↓
Bridge
      ↓
Herdr
      ↓
Existing PTY
      ↓
Agent
```

이를 **read-rich / write-raw** 모델로 정의한다.

### Explicit Rule

Transcript에는 절대 사용자 입력을 직접 append하지 않는다.

모든 입력은 agent process의 정상 interactive input path를 통과해야 한다.

---

## ADR-005 — PTY를 Interactive Truth로 유지한다

### Status

Accepted

### Context

Claude Code와 Codex CLI는 일반 메시지만 출력하지 않는다.

다음과 같은 terminal-native UI가 존재할 수 있다.

* interactive select
* permission prompt
* confirmation
* full-screen UI
* command palette
* progress renderer
* ANSI redraw
* special key handling

Transcript가 이러한 상태를 항상 완전히 표현한다고 보장할 수 없다.

### Decision

현재 interaction state에 대한 최종 source of truth는 PTY다.

```text
Transcript
  = conversation truth

PTY
  = interaction truth
```

Chat UI가 interaction을 안전하게 표현할 수 없으면 Terminal View로 fallback한다.

### Rule

불확실한 terminal output을 regex로 분석하여 임의의 structured action으로 만들지 않는다.

특히:

```text
Allow
Deny
Confirm
Choose option
```

등 실제 side effect를 발생시키는 interaction은 확실한 structured source가 있을 때만 native UI로 제공한다.

### Consequences

일부 interaction에서 Terminal로 이동해야 하지만 잘못된 command 입력보다 안전하고 유지보수성이 높다.

---

## ADR-006 — Agent Adapter Boundary를 둔다

### Status

Accepted

### Context

Claude Code와 Codex는 transcript 구조, session 위치, tool representation 등이 서로 다르다.

Client가 각각의 형식을 직접 알게 되면:

```text
Web UI
 ├ Claude parser
 └ Codex parser
```

형태로 coupling이 발생한다.

### Decision

agent별 차이는 server-side Adapter로 격리한다.

개념 contract:

```text
AgentAdapter

identifySession
readHistory
watchHistory
normalize
sendPrompt
interrupt
capabilities
```

초기 adapter:

```text
ClaudeCodeAdapter
CodexAdapter
```

현재 구현은 Claude adapter(`internal/claude`)뿐이다. `CodexAdapter`는 후속 작업이다
([P1-01](https://github.com/psw7205/herdr-remote/issues/4), [P1-02](https://github.com/psw7205/herdr-remote/issues/5)).

### Adapter Responsibilities

#### Session resolution

Herdr native session identity에서 실제 transcript를 찾는다.

#### Transcript decoding

agent-specific record를 읽는다.

#### Normalization

공통 event model로 변환한다.

#### Capabilities

해당 agent/version이 제공할 수 있는 기능을 노출한다.

예:

```text
chat_history
tool_events
structured_permission
structured_question
interrupt
attachments
```

### Non-Responsibility

Adapter가:

* 별도 agent를 실행하거나
* conversation DB를 만들거나
* Herdr lifecycle을 대신 관리

해서는 안 된다.

---

## ADR-007 — Unified Event Protocol을 Client Contract로 사용한다

### Status

Accepted. Note (2026-10-05): 예약한 `tool.started`·`tool.completed`·`tool.failed`는 쓰지 않는다. tool 호출은
Chat item(`message.tool`)이고 결과는 같은 item의 `message.updated`로 보낸다([ADR-039](#adr-039--tool-호출은-chat-item으로-투영하고-결과는-message-갱신으로-보낸다)).

### Context

Client가 Claude/Codex native transcript schema에 의존하면 agent update가 UI까지 전파된다.

또 reconnect, replay, notification 처리를 위해 일관된 event identity가 필요하다.

### Decision

Bridge와 Client 사이에는 normalized domain event protocol을 둔다.

최소 event category:

```text
session.*

agent.*

message.*

tool.*

permission.*

question.*

terminal.*

file.*
```

초기 event 종류는 현재 Bridge가 발행하는 구현됨 event와, 이름만 정하고 발행하지 않는
예약(미구현) event로 나뉜다.

구현됨:

```text
session.snapshot
session.error

agent.status

message.user
message.assistant
```

예약(미구현):

```text
message.assistant.delta

tool.started
tool.completed
tool.failed

permission.requested
permission.resolved

question.requested
question.resolved

file.changed

session.completed
```

### Event Envelope

모든 event에는 개념적으로 다음 metadata가 존재한다.

```text
event_id
session_id
sequence
timestamp
type
payload
source
```

### Requirements

#### Stable ordering

하나의 session 내에서 deterministic ordering을 제공해야 한다.

#### Idempotency

같은 event가 재전송되더라도 Client가 중복 rendering하지 않아야 한다.

#### Extensibility

Client는 알 수 없는 event type을 무시할 수 있어야 한다.

#### Versioning

protocol version을 독립적으로 관리할 수 있어야 한다.

예:

```text
protocol_version: 1
```

---

## ADR-008 — Snapshot + Live Event 모델을 사용한다

### Status

Accepted

### Context

모바일 client가 접속할 때 모든 과거 event를 처음부터 replay할 필요는 없다.

반대로 live event만 보내면 reconnect 후 누락 데이터를 복구하기 어렵다.

### Decision

Client sync는 두 단계로 구성한다.

```text
1. Snapshot
2. Live Events
```

#### Initial connection

```text
Client
   ↓
GET session snapshot
   ↓
Current messages
Current status
Current cursor
   ↓
WebSocket subscribe
```

#### Reconnect

```text
Client
   │ last cursor = 152
   ↓
Bridge
   │
   ├ replay 153...
   │
   └ subscribe live
```

Cursor는 `{epoch, sequence}`이며 snapshot과 cursor를 같은 session lock에서 읽는다.
`subscribe(after=C)`는 같은 lock에서 replay C+1…current를 확보하고 live 등록을 완료한다.
network 전송은 lock 밖에서 한다. queue overflow를 조용히 drop하지 않는다.

Bridge restart, transcript identity replacement, transcript 경로 이동, 복구 불가능한 resync, `ended`
session의 revive(watcher를 다시 열고 transcript를 처음부터 읽는다) 시 epoch를 교체한다.
다른 epoch, 미래 sequence, buffer miss는 fresh snapshot으로 복구한다.
이 결정은 Accepted ADR-019의 bounded buffer를 사용한다.

### Why

모든 상태를 event sourcing으로 만들 필요 없이 reconnect 안정성을 확보할 수 있다.

---

## ADR-009 — WebSocket은 Transport일 뿐 Session이 아니다

### Status

Accepted

### Context

모바일 browser에서는 다음 일이 일반적이다.

* background
* OS suspend
* Wi-Fi ↔ LTE 전환
* Tailscale reconnect
* browser tab discard

따라서 WebSocket connection은 자주 종료된다.

### Decision

WebSocket lifetime과 agent session lifetime을 완전히 분리한다.

```text
Agent Session
────────────────────────────────────────>

WS #1
──────X

             WS #2
             ─────────X

                           WS #3
                           ─────────────>
```

WebSocket disconnect는:

* agent interrupt
* pane close
* session close

를 발생시키지 않는다.

### Client Behavior

disconnect 시 UI는:

```text
Disconnected
Agent continues running
```

상태를 표시한다.

재접속 후 sync protocol을 실행한다.

---

## ADR-010 — Bridge는 Single Process Control Plane으로 시작한다

### Status

Accepted

### Context

초기 제품은:

* single user
* single host
* Tailnet only

를 목표로 한다.

Redis, message broker, distributed worker 등의 infrastructure는 필요하지 않다.

### Decision

초기 Bridge는 하나의 long-running server process로 구성한다.

논리적인 내부 component:

```text
Bridge

├── HerdrGateway
├── SessionRegistry
├── AgentAdapterRegistry
├── TranscriptWatcher
├── EventNormalizer
├── EventBuffer
├── TerminalGateway
├── AttachmentService
└── NotificationService
```

이들은 코드 구조상의 component이며 반드시 별도 process/service일 필요는 없다.
`AttachmentService`와 `NotificationService`는 ADR-015/017(Proposed)에 따른 후속 component이며
현재 구현되지 않았다.

### Persistence

최소 persistence만 사용한다.

예:

```text
UI preferences
read cursors
notification subscription
optional event cache
```

현재 구현된 persistence는 command receipt(`internal/command`, `-receipts-dir`)뿐이다.
UI preference, read cursor, notification subscription 저장은 해당 기능을 도입할 때 추가한다.

MVP에서 별도의 외부 DB server는 도입하지 않는다.

필요한 경우 embedded persistence를 우선한다.

### Rejected Alternative

```text
API server
Redis
worker
WebSocket gateway
database
event bus
```

형태의 distributed architecture.

현재 요구 대비 과도하다.

---

## ADR-011 — Client는 Thin Client로 유지한다

### Status

Accepted

### Context

Agent별 parsing logic이나 Herdr-specific logic이 browser에 들어가면 향후 native client를 추가할 때 동일 코드를 다시 구현해야 한다.

### Decision

Client는 normalized API만 사용한다.

Client responsibilities:

```text
session presentation
chat rendering
composer
terminal renderer
diff viewer
notifications UX
connection state
```

Client가 알아서는 안 되는 것:

```text
Claude transcript path
Codex JSONL schema
Herdr socket internals
agent process lookup
transcript parsing
```

### Consequence

나중에:

```text
Web
Native Android
Native iOS
Desktop
```

client를 추가하더라도 같은 Bridge contract를 사용할 수 있다.

---

## ADR-012 — Web/PWA를 첫 Client로 사용한다

### Status

Accepted

### Context

후보:

* Web / PWA
* React Native
* Flutter
* SwiftUI
* Jetpack Compose

프로젝트 특성:

* single user
* Tailnet only
* browser-compatible protocol
* terminal fallback 필요
* 빠른 iteration 중요
* native distribution 불필요

### Decision

MVP client는 mobile-first Web/PWA로 구현한다.

웹사이트가 아니라 installable application처럼 설계한다.

### Requirements

* standalone PWA
* mobile-first viewport
* safe area
* virtual keyboard 대응
* touch-first controls
* offline shell
* reconnect
* Web Push 가능한 범위에서 지원 (ADR-017 Proposed)

### Exit Criteria

다음 문제가 실제 사용에서 핵심 장애가 될 경우 native client를 검토한다.

* push reliability
* background execution
* share sheet integration
* file picker limitations
* keyboard UX
* OS integration

Bridge/API는 native migration과 독립적이어야 한다.

---

## ADR-013 — Terminal은 Chat과 동일 Session의 Secondary View다

### Status

Accepted

### Context

Terminal을 별도 화면이나 별도 connection 대상으로 취급하면 사용자가 같은 session인지 혼동할 수 있다.

### Decision

Session detail은 다음 구조를 가진다.

```text
Session

├── Chat
├── Changes
└── Terminal
```

`Changes`는 ADR-016(Proposed)이 채택될 때 추가하는 선택 화면이다. 현재 구현은 Chat과
Terminal만 제공하며(`web/src/route.ts`) `view=changes` URL은 Chat으로 처리한다.

**2026-10-05 note.** ADR-016이 Accepted되어 `view=changes`(변경된 파일 목록)와 `view=changes&file=<path>`
(파일 diff)를 제공한다. Back은 diff → 목록 → Chat 순서다. Changes는 binding이 필요 없어 read-only session에서도 열린다.

모든 화면(Chat, Terminal, 추가될 경우 Changes)은 동일한 SessionRef를 사용한다.

Terminal은 별도의 workspace/session을 생성하지 않는다.

### UX Principle

기본 진입:

```text
Session → Chat
```

Terminal은 명시적으로 이동한다.

Chat에서 unsupported interaction을 감지하면:

```text
Open Terminal
```

CTA를 제공한다.

---

## ADR-014 — File Editing은 제공하지 않고 Review까지만 지원한다

### Status

Accepted

### Context

agent 작업 결과를 모바일에서 확인할 필요는 있다.

그러나 editor까지 추가하면 제품이 빠르게 mobile IDE가 된다.

### Decision

Review·attachment 기능을 제공한다면 범위는 다음으로 제한한다. 이 기능들은
ADR-015/016(Proposed)에 따르며 현재 어느 것도 구현되지 않았다.

**2026-10-05 note.** changed files와 diff는 ADR-016(Accepted)으로 구현했다. basic file preview와
attachment upload는 여전히 미구현이다.

```text
changed files
diff
basic file preview
attachment upload
```

다음은 제공하지 않는다.

```text
source editing
git commit
git push
merge conflict resolution
repository browser IDE
```

### Reason

제품 목적은 coding agent와의 interaction이지 mobile coding environment가 아니다.

---

## ADR-015 — Attachment는 Host File로 변환한 뒤 Existing Agent에 전달한다

### Status

Proposed

후속 optional capability. 첫 slice 및 핵심 MVP의 필수 전제가 아니다.

### Context

모바일에서는 다음을 보내고 싶을 수 있다.

* screenshot
* image
* text file
* log
* PDF

하지만 이미 실행 중인 CLI agent에게 browser file object를 직접 전달할 방법은 없다.

### Decision

attachment는 Bridge host에 먼저 저장한다.

개념 흐름:

```text
Mobile
   │
   │ upload
   ▼
Bridge
   │
   ├── attachment storage
   │
   └── host path
          │
          ▼
Existing Agent Prompt
```

예:

```text
Please inspect this file:
/path/to/attachment/abc.png
```

Agent가 native image/file interaction을 지원한다면 adapter가 더 적합한 입력 방식으로 변환할 수 있다.

### Security

attachment path는:

* dedicated directory
* path traversal 방지
* size 제한
* filename sanitize

를 적용한다.

### Lifecycle

MVP에서는 temporary attachment로 취급하며 retention 정책은 추후 확정한다.

---

## ADR-016 — Changed Files는 Git을 Read-only Source로 사용한다

### Status

Accepted (2026-10-05). [P2-03](https://github.com/psw7205/herdr-remote/issues/16).

후속 optional capability. 첫 slice 및 핵심 MVP의 필수 전제가 아니다.

**2026-10-05 note — 구현 경계.** 아래 Decision을 다음 경계로 구현했다.

- Repository는 registry item의 `project`(Herdr가 보고한 cwd)에서 `git rev-parse --show-toplevel`로 찾는다.
  client는 directory를 보내지 않는다. `ended`·`unbound` item도 registry에 남아 있으면 읽을 수 있다.
  Git 읽기는 agent write가 아니고 binding과 무관하기 때문이다. Git repository가 아니면 오류가 아닌
  `repository: false`다.
- 비교 기준은 HEAD다(index와 working tree를 합친 상태). commit이 없으면 empty tree와 비교한다.
  목록은 `git status --porcelain=v2 -z`, 줄 수는 plumbing `git diff-index --numstat`, 파일 diff는
  `git diff-index -p`다. porcelain `git diff <commit>`은 `--no-optional-locks`에서도 index를 다시 쓰므로
  쓰지 않는다. untracked file은 추가로 보고 Bridge가 직접 읽어 줄 수를 센다.
- diff 요청의 `path`는 같은 요청에서 다시 읽은 status 목록의 path와 정확히 같을 때만 git에 전달한다.
  `--literal-pathspecs`와 `--` 뒤에 둔다.
- repo config가 명령을 실행하지 못하게 한다. `core.fsmonitor=false`, `core.hooksPath`, config에 정의된
  모든 `filter.<driver>`의 clean·smudge·process 비우기를 command scope(`GIT_CONFIG_COUNT`)로 덮고,
  `--no-ext-diff`·`--no-textconv`·`--ignore-submodules=dirty`를 쓴다. 환경은 고정 목록만 넘긴다.
  `GIT_CONFIG_COUNT`를 모르는 git 2.31 미만은 이 덮기가 빠지므로 시작 시 거부하고 Changed Files를 끈다.
  system·global config는 남긴다(`core.excludesFile` 등). 그 결과 Git LFS 같은 filter가 있는 파일은 stat이
  바뀐 경우 filter 없이 비교된다.
- untracked file은 `os.Root` 안에서 Lstat한 regular file이고, 연 file이 같은 file일 때만 읽는다.
  symlink와 FIFO는 읽지 않는다.
- 목록은 500개, 파일 diff는 256 KiB에서 자르고 `truncated`로 표시한다. git 실행은 한 번에 2개, 각 10초다.
- source 수정, stage, commit, push UI는 없다(ADR-014). Bridge의 polling이나 WS event는 없고 client가
  화면을 열거나 새로 고칠 때만 읽는다.

### Context

Conversation transcript만으로는 agent가 실제 어떤 파일을 수정했는지 정확하게 알기 어렵다.

### Decision

Changes View는 repository Git state를 직접 읽는다.

예상 정보:

```text
modified files
added files
deleted files
diff
line additions/deletions
```

Agent transcript의 tool event에서 변경 파일을 추론하지 않는다.

### Important Boundary

Git state 전체가 특정 agent 작업만을 의미한다고 가정하지 않는다.

동일 worktree에서 사람이 수정한 내용도 포함될 수 있다.

따라서 UI 표현은:

```text
Changed files
```

로 하고:

```text
Files changed by Codex
```

라고 단정하지 않는다.

Herdr가 agent별 worktree isolation을 보장하는 경우 추후 더 정확한 attribution을 제공할 수 있다.

---

## ADR-017 — Push Notification은 Derived Event로 취급한다

### Status

Proposed

후속 optional capability. 첫 slice 및 핵심 MVP의 필수 전제가 아니다.

### Context

notification 자체가 session state의 source가 되어서는 안 된다.

notification delivery는 실패할 수 있다.

### Decision

notification은 normalized event에서 파생한다.

초기 notification trigger:

```text
needs_attention
completed
```

예:

```text
agent.status → needs_attention
        ↓
NotificationService
        ↓
Web Push
```

notification을 탭하면 해당 `session_id`로 deep-link한다.

### Rule

notification을 받지 못하더라도 앱을 열면 모든 상태를 정상 복구할 수 있어야 한다.

---

## ADR-018 — Attention State는 Domain-level Derived State로 관리한다

### Status

Accepted

### Context

Herdr state와 transcript event를 그대로 UI에 노출하면 사용자는 여러 기술적 상태를 해석해야 한다.

모바일에서는 "내가 지금 해야 할 일이 있는가?"가 더 중요하다.

### Decision

Bridge에서 presentation-oriented status를 계산한다.

```text
needs_attention
working
idle
completed
error
```

입력 source:

```text
Herdr agent state
+
recent normalized events
+
adapter capabilities
```

### Priority

```text
error
needs_attention
working
completed
idle
```

정확한 priority는 실제 사용 후 조정 가능하다.

### Constraint

불확실한 terminal text parsing만으로 `needs_attention`을 발생시키지 않는다.

---

## ADR-019 — Local Event Buffer는 제한적으로 유지한다

### Status

Accepted

### Context

reconnect replay를 위해 최근 event를 보존할 필요가 있다.

하지만 전체 conversation을 새로운 event-store DB에 복제하고 싶지는 않다.

### Decision

Bridge는 bounded event buffer를 유지한다.

목적:

* short disconnect replay
* duplicate suppression
* stream continuity

Source of truth가 아니다.

예:

```text
recent N events

or

recent N minutes
```

첫 구현은 session별 최근 1,024 events 또는 payload 8 MiB 중 먼저 도달하는 한도를 사용한다.
한 event가 한도를 초과하면 replay 대신 snapshot 재동기화를 사용한다.
Subscriber queue는 128 events로 제한하며 overflow는 연결 종료/재동기화를 유발한다.

buffer miss 시 transcript에서 새로운 snapshot을 만든다.

```text
cursor available
    ↓
replay

cursor expired
    ↓
new snapshot
```

### Consequence

event sourcing infrastructure 없이 reconnect를 구현할 수 있다.

---

## ADR-020 — Transcript Watcher는 Incremental Parsing을 사용한다

### Status

Accepted

### Context

Agent transcript가 커질 경우 매 polling마다 전체 파일을 재parse하는 것은 비효율적이다.

또 agent가 파일에 데이터를 append하는 중 partial record를 읽을 수 있다.

### Decision

각 active transcript에 대해 incremental cursor를 유지한다.

개념:

```text
file identity
offset
partial buffer
last normalized sequence
```

Watcher는:

1. 변경 감지
2. 마지막 offset 이후 bytes 읽기
3. complete record만 parse
4. incomplete tail은 buffer
5. normalized event 생성
6. offset 갱신

을 수행한다.

### File Replacement

agent가 transcript rotate/rewrite를 할 가능성을 고려한다.

file identity가 바뀌면 adapter가 재동기화한다.

Claude Code는 session이 working directory를 옮기면(예: worktree 진입) transcript를 다른 project
directory로 옮긴다. Bridge는 주기적 discovery마다 native session ID로 경로를 다시 찾고, 바뀌면 새
경로를 처음부터 읽어 epoch를 교체한다. 같은 ID가 두 곳에서 보이면 기존 경로를 유지한다.

Watcher는 transcript 파일만 감시하고 directory를 감시하지 않는다. macOS kqueue는 감시한 directory의
파일마다 descriptor를 연다. 파일 감시는 삭제·이동·교체 뒤 다시 등록하며, 알림을 놓치거나 쓸 수 없을
때는 polling이 offset 이후를 읽는다.

---

## ADR-021 — Full Transcript는 필요할 때 Snapshot으로 재구성한다

### Status

Accepted

### Context

Bridge가 모든 conversation을 별도 DB에 복제하지 않기 때문에 initial load를 처리하는 방식이 필요하다.

### Decision

Session 진입 시 adapter가 native transcript에서 현재 conversation snapshot을 생성한다.

```text
Native transcript
       ↓
parse
       ↓
normalized messages
       ↓
snapshot
```

이후 live update는 incremental watcher를 사용한다.

### Optimization

실제 성능 문제가 확인될 때만:

* parsed transcript cache
* index
* incremental snapshot cache

를 추가한다.

사전에 별도 conversation database를 만들지 않는다.

---

## ADR-022 — Structured Actions에는 Capability Negotiation을 사용한다

### Status

Accepted

### Context

Claude와 Codex의 version에 따라 structured permission/question event 지원 수준이 다를 수 있다.

UI가 항상 기능이 있다고 가정해서는 안 된다.

### Decision

각 session은 capability 정보를 제공한다.

예:

```text
chat: true
terminal: true
interrupt: true

structured_permission: false
structured_question: true

attachments:
  image: true
  file: true
```

`attachments`는 ADR-015(Proposed)가 채택될 때의 capability 예시다.

Client는 capability 기반으로 UI를 노출한다.

### Result

새 agent/version을 추가해도 unsupported functionality를 억지로 구현할 필요가 없다.

---

## ADR-023 — Interrupt는 Prompt와 별도 Command로 모델링한다

### Status

Accepted

### Context

`Ctrl-C` 문자열을 일반 prompt와 동일하게 취급하면 semantic 의미가 사라진다.

### Decision

Bridge API에는 명시적인 interrupt command가 존재한다.

```text
POST /api/sessions/{id}/commands
command_type: "interrupt"
```

ADR-033의 command envelope를 그대로 사용하며, Bridge는 이를 binding 검증 경로
(`agent.bound_input`)로 Herdr에 전달하고 Herdr가 해당 PTY에 맞는 입력으로 변환한다.

Client는:

```text
Send
Stop
```

을 별도 action으로 표현한다.

---

## ADR-024 — Tailnet을 Primary Security Boundary로 사용한다

### Status

Accepted

### Context

MVP는 개인이 자신의 Herdr host에 접속하는 도구다.

공개 인터넷 exposure가 필요하지 않다.

### Decision

기본 topology:

```text
Bridge
  bind:
  localhost

      ↓

Tailscale Serve

      ↓

Tailnet HTTPS
```

Bridge를 `0.0.0.0`으로 직접 공개하는 구성을 기본으로 하지 않는다.

### Application Authentication

MVP에서는:

* user DB
* password login
* OAuth
* refresh token

을 구현하지 않는다.

추가 app lock은 별도 threat model이 필요할 경우 검토한다.

### Security Assumption

Tailnet membership만으로 browser control API 호출을 신뢰하지 않는다.
보안 경계는 Tailnet + 최소 권한 ACL + exact browser Origin 검증이다.
ACL이 넓은 배포에서는 Serve가 검증해 추가하는 `Tailscale-User-Login`을
Bridge가 지정한 소유자와 대조한다. Serve는 클라이언트가 보낸 동일 header를 제거하며
Bridge는 localhost에만 bind한다. 이 검증은 자체 login/password 시스템이 아니다.
localhost bind를 기본으로 하며 mutation과 WS handshake는 explicit Origin allowlist를 적용한다.
wildcard CORS/null Origin을 허용하지 않는다. HTTP mutation은 JSON content type을 요구하고
Host도 명시한 배포 origin과 대조한다. README 배포 절차에 Tailscale ACL 최소 권한 구성을 포함한다.

### Owner 자동 감지 기본값

Status: Accepted (구현됨. 실제 mobile 검증의 남은 항목은
[P0-02](https://github.com/psw7205/herdr-remote/issues/1)에서 추적)

설치자가 host와 login을 직접 맞추지 않도록 Bridge는 시작 시 한 번 `tailscale status --json`을
조회한다. 기본 host는 `Self.DNSName`에서 끝의 점을 제거한 값이고, 기본 소유자는 이 Tailscale
node를 소유한 사용자의 login이다. `-tailnet-host`·`-tailnet-login`의 명시 값이 자동 값보다
우선하며 `-tailnet-host off`는 Tailnet 요청을 받지 않는다. 이 값은 identity 대조 대상일 뿐
app-level 인증을 추가하지 않는다. tagged node나 login이 없는 node처럼 소유자를 알 수 없으면
host만 유지하고 모든 tailnet 요청을 거부한다. CLI가 없거나 실패하면 `auto` host는 확정되지
않아 localhost 전용으로 동작한다. host를 명시했는데 `auto` login만 실패한 경우에는 host를
유지하고 모든 tailnet 요청을 거부하며, localhost 전용으로 바뀌지 않는다. 어느 쪽이든 Bridge는
종료하지 않는다. Serve는 client의 `Host`를 그대로 전달하므로 localhost 판정은 `Host`만 보지
않는다. Tailnet host·Origin과 일치하거나 Serve가 추가하는 `X-Forwarded-For`·`X-Forwarded-Host`·
`X-Forwarded-Proto`·`Tailscale-User-Login`·`Tailscale-User-Name` 중 하나라도 있는 요청은 host 확정
여부와 무관하게 tailnet 요청이며, host와 owner login이 확정되고 `Tailscale-User-Login`이 owner와
일치하며 `Host`가 tailnet host일 때만 허용한다. `https://<host>`는 host와 owner login이 모두 확정된 경우에만 Origin
allowlist에 추가되며, login이 없는 동안 `-origins`로 넣은 tailnet HTTPS Origin은 경고와 함께
제외된다. `doctor`의 Funnel 감지는 host의 모든 port와 `--bg` 없이 실행한 foreground Serve
설정까지 포함한다. 어느 경우에도 Serve 사용에는 tailnet 관리 화면에서 HTTPS Certificates를 한 번
활성화해야 한다.

기각한 대안:

* Tailnet IP에 HTTP로 직접 bind: peer identity는 `WhoIs`로 확인할 수 있지만 insecure
  browser context가 되어 service worker/PWA, clipboard API, `crypto.randomUUID`가 동작하지 않는다.
* `tsnet` 내장: 여전히 tailnet의 HTTPS Certificates가 필요하고, 큰 `tailscale.com` dependency와
  node state 관리가 추가되며, 줄이는 것은 `tailscale serve` 명령 하나다. Serve 설정 마찰이
  실제로 관찰되면 다시 검토한다.

---

## ADR-025 — Host는 MVP에서 하나만 지원한다

### Status

Accepted

### Context

multi-host를 지원하면 모든 identity에 host routing이 필요해진다.

또:

* host discovery
* authentication
* connection health
* cross-host session list

등이 추가된다.

### Decision

MVP deployment는:

```text
1 Bridge
1 Herdr instance
1 host
```

다.

내부 identity에 future compatibility를 위해 `host_id`를 둘 수 있지만 실제 multi-host routing은 구현하지 않는다.

---

## ADR-026 — Bridge API는 UI 프레임워크에 독립적이어야 한다

### Status

Accepted

### Context

첫 client는 PWA지만 향후 native app 전환 가능성을 남겨야 한다.

### Decision

Bridge API에서 다음을 사용하지 않는다.

* browser-specific object
* React-specific state
* DOM concepts
* service-worker-specific concepts

프로토콜은 일반적인:

```text
HTTP
WebSocket
JSON
binary upload
```

형태로 정의한다.

이를 통해 향후:

```text
SwiftUI
Jetpack Compose
Desktop
CLI
```

client에서도 동일 API를 사용할 수 있다.

---

## ADR-027 — REST/HTTP + WebSocket Hybrid Transport를 사용한다

### Status

Accepted

### Context

모든 요청을 WebSocket RPC로 처리할 수도 있지만 snapshot, upload, diff 등은 request-response 형태가 더 자연스럽다.

### Decision

두 transport를 역할에 따라 나눈다.

#### HTTP

사용:

```text
session list
session snapshot
diff
file preview
attachment upload
commands requiring request/response
```

#### WebSocket

사용:

```text
live events
status changes
streaming messages
reconnect replay
terminal stream
```

Terminal stream은 필요에 따라 별도 WebSocket channel을 사용할 수 있다.

### Reason

WebSocket 하나에 모든 API를 억지로 넣는 것보다 debugging과 client 구현이 단순하다.

---

## ADR-028 — Terminal Stream과 Semantic Event Stream을 논리적으로 분리한다

### Status

Accepted

### Context

Terminal output은:

* 매우 높은 빈도
* binary/ANSI
* redraw 포함
* chat event보다 데이터량 큼

반면 semantic events는:

* 낮은 빈도
* durable 의미
* replay 필요

특성이 다르다.

### Decision

두 stream을 별도 logical channel로 다룬다.

```text
Semantic Channel
  reliable-ish
  ordered
  reconnect/replay

Terminal Channel
  live
  ephemeral
  no historical guarantee
```

실제 transport를 WebSocket 하나로 multiplex할지 두 connection으로 나눌지는 구현 단계에서 결정할 수 있다.

하지만 domain semantics는 분리한다.

---

## ADR-029 — Server Restart를 Agent Session Failure로 취급하지 않는다

### Status

Accepted

### Context

Bridge는 projection layer이므로 restart될 수 있다.

Herdr와 agent는 계속 살아 있을 수 있다.

### Decision

Bridge startup 시 현재 Herdr state를 다시 discover한다.

```text
Bridge start
   ↓
connect Herdr
   ↓
discover panes
   ↓
resolve native sessions
   ↓
rebuild session registry
   ↓
reattach transcript watchers
```

Bridge persistence가 없더라도 conversation을 native transcript에서 다시 구성할 수 있어야 한다.

### Goal

Bridge를 언제든 재시작할 수 있어야 한다.

---

## ADR-030 — Graceful Degradation을 핵심 Compatibility Strategy로 사용한다

### Status

Accepted

### Context

Agent CLI는 자주 업데이트될 수 있다.

Transcript format이나 interactive UI가 변경될 수 있다.

### Decision

기능 실패를 session 전체 실패로 확대하지 않는다.

예:

```text
structured tool parsing fails
        ↓
plain assistant message available

question parser fails
        ↓
terminal fallback

transcript unavailable
        ↓
terminal-only mode
```

최소 capability:

```text
terminal
```

이 살아 있다면 session은 계속 사용할 수 있다.

### Compatibility Hierarchy

```text
Rich Chat
   ↓
Basic Chat
   ↓
Terminal
```

---

## ADR-031 — MVP 구현 순서는 Vertical Slice를 따른다

### Status

Accepted. Completed (2026-09-24): Slice 1~6을 구현했다. Slice 7·8은 [P2-03](https://github.com/psw7205/herdr-remote/issues/16)·[P2-05](https://github.com/psw7205/herdr-remote/issues/18)에서
착수 조건이 생길 때 다룬다.

### Context

Bridge, transcript parser, UI를 각각 완성한 뒤 통합하면 실제 사용 검증이 늦어진다.

### Decision

다음 순서로 vertical slice를 구현한다.

#### Slice 1 — Session discovery

```text
Herdr
 → Bridge
 → mobile session list
```

Success:

실행 중 agent를 모바일에서 정확하게 확인할 수 있다.

#### Slice 2 — Read-only Chat

```text
Herdr session ID
 → transcript
 → adapter
 → normalized history
 → Chat
```

Success:

PC에서 진행한 conversation을 모바일에서 읽을 수 있다.

#### Slice 3 — Write

```text
Composer
 → Bridge
 → Herdr PTY
 → Agent
 → Transcript
 → Chat
```

Success:

모바일 메시지가 기존 session에 나타난다.

#### Slice 4 — Live updates

```text
transcript watcher
 → event stream
 → live UI
```

#### Slice 5 — Reconnect

background / foreground recovery.

#### Slice 6 — Terminal fallback

#### Slice 7 — Changes / diff

#### Slice 8 — Notifications

각 단계는 실제 모바일 사용이 가능한 상태로 유지한다.

---

## ADR-032 — MVP 완료 기준은 Architecture가 아니라 실제 Handoff Flow다

### Status

Accepted. Completed (2026-09-24): 첫 vertical slice의 handoff flow를 실측했다
([검증 기록](records/verification.md#첫-vertical-slice-검증-2026-09-24)). 실기기 reconnect는 [P0-02](https://github.com/psw7205/herdr-remote/issues/1)에서 추적한다.

### Decision

첫 vertical slice가 성공했다고 판단하려면 다음 end-to-end scenario가 동작해야 한다.

```text
1. 이미 실행 중인 Desktop Herdr agent 사용

2. Claude session에서 작업 시작

3. Mobile PWA 실행

4. 같은 Claude session 확인

5. 기존 conversation 확인

6. 모바일에서:
   "테스트까지 진행해줘"
   입력

7. 기존 Claude process가 입력 수신

8. Desktop terminal에서도 동일 conversation 확인

9. Mobile screen lock

10. Claude 작업 계속

11. 작업 완료

12. Mobile foreground

13. 결과 자동 복구

14. conversation 복구 확인 (Changed Files는 후속 optional)

15. 필요 시 Terminal View 진입
```

다음 중 하나라도 발생하면 핵심 architecture 결함으로 본다.

```text
duplicate Claude process

different conversation

lost messages after reconnect

wrong transcript attached

client disconnect kills process
```

---

## ADR-033 — Command receipt로 retry 중복 실행을 방지한다

### Status

Accepted

### Decision

Command envelope는 `command_id`, `session_id`, `runtime_binding`, `command_type`,
`payload`다. `prompt`, `interrupt`, `terminal_input`은 별도 command type이다.

Bridge는 ID와 canonical request digest를 durable하게 reserve한 뒤 한 번만 dispatch한다.
동일 ID/digest의 동시 요청과 retry는 동일 receipt를 반환한다. 다른 payload/target은 conflict다.
Browser는 전송 전에 ID를 session-scoped presentation state에 저장한다. timeout이나
reload 후 같은 초안·binding을 다시 보낼 때 ID를 재사용하고, 내용이나 binding이 바뀌면
전달 불확실 상태를 사용자가 확인할 때까지 새 command를 만들지 않는다.
저장 실패는 전송 전에 reject한다. pending receipt가 남은 채 restart하면 `delivery_unknown`으로
복구하고 자동 재전송하지 않는다. 전송 후 timeout이나 partial failure도 unknown이다.
만료 receipt를 삭제한 뒤 같은 ID를 새 command처럼 수락하지 않는다. retention을 적용할 때는
만료 namespace 전체를 거부하는 규칙을 함께 둔다.

결과는 `accepted`, `rejected`, `delivery_unknown`이다. accepted는 PTY 전달 결과이며
agent turn 성공이나 conversation 저장 완료를 뜻하지 않는다. message 확정은 transcript에서만 한다.
receipt는 conversation 본문을 저장하는 DB가 아니며 command digest와 최소 상태만 유지한다.
이 설계는 at-most-once dispatch이며 원격 process의 exactly-once 실행을 주장하지 않는다.

---

## ADR-034 — Herdr가 runtime binding을 검증한 command만 전달한다

### Status

Accepted (stock Herdr `0.9.1`에는 미포함, 소유 fork `psw7205/herdr`의 `mobile-binding` patch에서
구현. [ADR-036](#adr-036--herdr-patch는-소유-fork에서-유지한다))

2026-10-05 note: `unbound`는 binding이 없는 `codex:<native-id>` item에도 쓴다. Codex의 identity와
lifecycle은 [ADR-038](#adr-038--codex-chat은-herdr가-보고한-native-session으로-read-only-표시한다)을 따른다.

### Decision

Herdr가 native session, foreground process incarnation, terminal을 결합한 opaque binding을
발급한다. 모든 semantic/raw write는 client가 본 binding을 전달하며 mismatch/ended는 reject한다.
Bridge의 get-then-write만으로 검증을 대체하지 않는다. API handler뿐 아니라 queue에서 실제
input을 처리할 때도 binding을 검증하고 종료·교체 시 queued input을 무효화한다.
text/Enter 중 일부가 전달되었으면 `delivery_unknown`으로 반환한다.

`pane_id`, `terminal_id`, `revision`, `state_change_seq`는 단독 binding이 아니다.
Herdr capability가 없거나 native identity가 불명확하면 write를 fail closed한다.
Bridge는 실행 중 server의 조건부 입력 지원을 `supported`/`unsupported`/`unknown`으로 판정해
`/api/sessions`의 `herdr.conditional_input`과 `doctor`에 노출한다. stock과 patch가 같은 version
문자열을 쓰므로 version으로 판정하지 않는다. 운영 절차는 `docs/herdr-patch.md`를 따른다.
process 종료와 OS PTY 수신 사이 race까지 무조건 해결했다고 가정하지 않으며
실제 process 교체 acceptance를 통과하기 전에는 write-enabled handoff 완료로 표시하지 않는다.

#### Binding·capability 손실은 lifecycle 종료가 아니다

Herdr capability 부재(`ErrUnsupported`)나 일시적 binding 오류로 검증된 binding을 잃어도 기존
`claude:<native-id>` session은 목록에 남고 lifecycle은 `unverified`다. `runtime_binding`을 비우므로
prompt, interrupt, Terminal read/input은 모두 fail closed한다. `unverified`는 검증됐던 session에만
쓴다. native session을 식별하지 못한 `pane:<pane-id>` item은 binding이 없으면 `unbound`다.

Continuity 규칙: active `claude:<native-id>` item은 Herdr가 같은 `pane_id`에서 `claude`를 계속
보고하고, Herdr `agent_session`과 성공한 `agent.binding` 결과의 native ID가 없거나 같은 native ID일
때만 유지한다. terminal_id race 등으로 걸러진 binding도 다른 native ID를 가리키면 다른 session의
증거다. 한 Refresh에서는 검증된 관찰이 먼저 ID를 차지하고, continuity 관찰은 아직 차지되지 않은
ID만 이어받는다. 한 pane에 active item이 둘 이상이면(snapshot이 같은 pane을 중복 보고한 경우)
continuity를 적용하지 않는다. 한 Refresh에서 같은 native ID가 둘 이상의 pane에서 검증되면(예: 실행 중인
session을 다른 pane에서 `claude --resume`) 어느 관찰도 binding과 함께 `claude:<native-id>`를 차지하지
않고 read-only continuity 규칙으로만 처리하며, 그 관찰로는 supersession도 일어나지 않는다. `terminal_id`는 live handoff에서 재발급되고 cwd는 같은 repository의
session끼리 공유하므로 continuity 신호로 쓰지 않는다.

`ended`는 pane이 snapshot에서 사라졌거나, Herdr가 그 pane에서 claude를 더 이상 보고하지 않거나,
`agent_session` 또는 검증된 binding이 다른 native ID를 가리킬 때만 기록한다. 같은 native ID의
binding이 다시 검증되면 새 binding으로 `active`에 복귀하며, 이미 `ended`인 item도 이 경로로만
복귀한다. `ended` item에는 `unverified` continuity를 적용하지 않는다.

Supersession 규칙: `pane:<pane-id>` item이 목록에서 빠지는 Refresh에서 같은 `pane_id`에 검증된
`claude:<native-id>` item이 새로 active가 되면(신규, `ended`에서 복귀, 다른 pane에서 이동) `pane:` item은 `ended`
대신 `superseded`가 되고 `Meta`와 `agent.status`에 `successor_id`를 싣는다. successor의 binding은 그
관찰에서만 가져오며 `pane:` item의 binding은 비워 reject한다. client는 열린 Chat·Terminal을
successor로 전환한다. snapshot이 같은 pane을 중복 보고했거나 한 pane에 active item이 둘 이상이면 그 pane의
`pane:` item은 supersede되지 않는다.

알려진 한계:

* Herdr hook이 `agent_session`을 보고하지 않으면 한 refresh 주기(2s) 안의 같은 pane claude→claude
  교체를 구분하지 못한다. 이때 이전 session의 transcript를 read-only로 계속 보여 주며 write는 불가능하다.
* agent 감지가 순간적으로 빠지면 item은 `ended`가 된다.
* supersession은 pane 단위 판정이다. `pane:` item의 process가 같은 pane의 다른 process로 바뀐 뒤
  검증되어도 `superseded`로 보인다. write는 successor 자신의 binding으로만 가능하다.

---

## ADR-035 — Mobile Terminal은 기존 PTY의 passive mirror다

### Status

Accepted (관찰은 기존 visible ANSI snapshot, 입력은 ADR-034 필요)

### Decision

Herdr/desktop이 PTY size owner다. mobile observer attach는 start, resume, resize, takeover를
수행하지 않고 여러 observer를 허용한다. detach는 observer만 제거한다.
Raw terminal input에도 ADR-034 binding 보호와 ADR-033 retry 의미를 적용한다.

현재 Herdr direct attach는 resize와 pending resume 경로를 포함하므로 그대로 proxy하지 않는다.
첫 slice의 관찰은 공개 `pane.read`의 `visible` + `ansi` snapshot을 사용한다. 이는 raw PTY
byte stream이 아니며 client는 frame을 교체 표시한다. intermediate frame/history를 보장하지 않는다.
이 경로는 attach owner를 점유하거나 resize/resume/start하지 않는다. `recent` text read의
interactive history collection으로 대체하지 않는다. renderer만 붙여 interactive fallback 완료로
표시하지 않으며 ADR-034 조건부 입력까지 검증해야 한다.

Terminal frame 응답의 `cols`·`rows`는 additive field다. Bridge가 read-only `pane.layout`의
pane `rect`(zoomed tab의 focused pane이면 tab `area`)에서 채우며, 읽지 못하면 생략한다.
이 값은 client가 frame을 그릴 render grid이고 Herdr PTY resize나 PTY 크기 보장이 아니다.
`rect`는 border·scrollbar를 포함한 상한이고 direct attach resize lock이 있으면 PTY와 다를 수
있으므로 client는 이 값과 frame의 줄 수·가장 긴 줄 폭 중 큰 값을 grid로 쓴다. mobile viewport는
font size와 horizontal scroll로만 맞추며 PTY 크기를 바꾸지 않는다.

---

## ADR-036 — Herdr patch는 소유 fork에서 유지한다

### Status

Accepted (2026-09-28)

### Context

ADR-034의 조건부 입력에는 stock Herdr에 없는 `agent.binding`·`agent.bound_input`이 필요하다.
Herdr 원본(`herdrdev/herdr`)은 승인되지 않은 외부 contributor의 feature PR을 자동으로 닫고,
기능 제안은 maintainer 승인을 거쳐야 한다. upstream 반영을 전제로 할 수 없다.

### Decision

patch는 소유 fork `psw7205/herdr`에서 유지한다. fork `master`는 `upstream/master`의
fast-forward mirror이고, patch는 최신 stable tag 위의 `mobile-binding` branch에 둔다.
stable release마다 rebase와 `just ci`·설치 검증을 거쳐 갱신한다. 절차는
[Herdr patch runbook §8](herdr-patch.md#8-fork-관리)을 따른다.

기각한 대안:

- upstream PR: contribution 정책상 받아들여질 경로가 없다.
- stock `agent.prompt`·raw pane input 사용: stale binding 보호가 없어 ADR-034를 위반한다.

### Consequences

사용자는 fork의 patched Herdr를 직접 build·설치해야 한다. Herdr release를 따라가는 rebase
비용은 이 project가 진다. Bridge는 patch 유무를 version이 아니라 `doctor`의
`conditional_input`으로 판별하므로(P0-04) stock으로 바뀌어도 fail closed한다.

---

## ADR-037 — 새 session 생성은 Herdr에 요청한다

### Status

Accepted (2026-09-28). 2026-09-29 구현, [P1-14](https://github.com/psw7205/herdr-remote/issues/13)

### Context

ADR-001은 자체 agent runner와 기존 session resume을 거부했다. 두 경우 모두 같은 agent에
lifecycle owner가 둘이 되거나 같은 conversation에 process가 둘 붙는다. 이 결정이 요약 문서로
옮겨지면서 "Bridge는 agent를 start하지 않는다"는 절대 금지로 일반화됐지만, 사용자가 명시적으로
요청해 Herdr가 새 native session을 만드는 경우는 검토한 적이 없다.

Herdr 공개 API는 `workspace.create`, `tab.create`, `agent.start`로 Herdr가 소유하는 pane에서
agent를 시작할 수 있다. 실행 중 agent의 cwd를 바꾸는 API는 없다. 조사 기록은
[2026-09-28 새 session 생성 API 조사](records/integration-findings.md#2026-09-28--새-session-생성-api-조사)에 있다.

### Decision

사용자의 명시적 요청이 있을 때 Bridge는 Herdr 공개 API로 새 session 생성을 요청한다.
process는 Herdr가 시작하고 소유한다. Bridge가 process를 직접 실행하거나, shell pane에
`pane.send_text`로 실행 명령을 입력하지 않는다.

**흐름.** 새 workspace는 `workspace.create(cwd)`의 root pane에서, 이미 열린 workspace는
`tab.create(workspace_id, cwd)`의 root pane에서 `agent.start`를 호출한다. 생성은 focus를 바꾸지
않고 `env`를 전달하지 않는다. agent name은 Bridge가 `[a-z][a-z0-9_-]{0,31}` 안에서 만든다.

**Kind와 인자.** `kind`는 allowlist로 받는다. 첫 허용값은 `claude`다. 다른 kind는 해당
adapter의 transcript와 조건부 입력이 검증된 뒤에만 추가하며(Codex는 P1-01/02), 허용 여부는
ADR-022 capability로 노출한다. `agent.start`의 `--` 뒤 인자는 받지 않는다. 인자를 받으면
`--resume`·`--continue`로 ADR-001이 거부한 resume이 다시 가능해지고, permission 우회 옵션도
원격에서 켤 수 있기 때문이다.

**폴더 후보.** Bridge는 `-project-root` flag(반복 가능)로 root를 받는다. root가 Git repo면 root
하나가 후보이고, 아니면 바로 아래 한 단계의 Git repo(`.git` directory 또는 file)만 후보다.
hidden directory는 제외한다. symlink를 해석한 실제 경로가 root 밖이면 제외한다. 여기에 Herdr의
열린 workspace를 합치고 "열려 있음"으로 표시한다. `workspace.list`에는 cwd가 없으므로 workspace의
폴더는 `session.snapshot`에서 그 workspace 첫 pane의 cwd로 읽는다. 열린 workspace를 고르면
기본 동작은 그 workspace에 새 tab을 여는 것이다. 같은 cwd의 workspace를 중복으로 만들지 않는다.
root가 없으면 새 workspace 생성은 비활성이다.

**경로 경계.** client는 절대 경로를 보내지 않는다. 후보는 server가 발급한 opaque ID로만
식별하고 server가 경로로 해석한다. dispatch 직전에 경로를 다시 `EvalSymlinks`하여 root 안의
directory인지(열린 workspace라면 여전히 열려 있는지) 확인하고, 실패하면 reject한다.
host filesystem 탐색, 임의 경로 입력, transcript에서 추출한 최근 project, Herdr 내부 state
파일 읽기는 제공하지 않는다.

**API.** 후보는 `GET /api/start-candidates`, 생성은 `POST /api/sessions`(`command_type: "session_start"`)다.
허용 kind와 새 workspace 가능 여부는 `GET /api/sessions`의 `start`로 노출한다.

**중복 방지.** 생성은 새 command type `session_start`이며 ADR-033 receipt를 따른다. digest는
후보 ID, kind, 배치(new workspace 또는 기존 workspace의 new tab)로 만든다. `runtime_binding`은
없다. receipt에는 생성된 workspace·tab·pane ID를 함께 남겨 같은 ID의 retry가 같은 결과를
반환한다. Herdr 호출과 receipt 기록 사이의 timeout이나 crash는 `delivery_unknown`이며 자동
재시도하지 않는다. workspace는 생성됐지만 `agent.start`가 실패하면 생성된 ID와 실패를 함께
반환하고, 만든 자원을 자동으로 닫거나 다시 시도하지 않는다.

**생성 이후.** 새 session은 특별 경로 없이 기존 discovery를 따른다. `pane:<pane-id>`로 보이고
native session이 검증되면 `claude:<native-id>`가 된다. Chat·prompt·Terminal 입력은 binding이
검증된 뒤에만 열린다(ADR-034, ADR-035).

### Consequences

* PC 없이 모바일에서 작업을 시작할 수 있다. owner는 여전히 Herdr 하나다.
* Herdr가 실행 중이어야 한다. 새 workspace는 `-project-root`를 설정해야 쓸 수 있다.
* root 아래 directory 이름이 소유자 identity 경계 안에서 노출된다. root는 agent를 실행해도 되는
  폴더라는 신뢰 경계이기도 하다. 폴더 안 설정(hooks, MCP 등)은 agent 실행 시 동작할 수 있다.
* desktop client가 붙지 않은 상태에서 만든 pane은 Herdr fallback 크기를 쓸 수 있다(P1-08).
  모바일은 여전히 PTY를 resize하지 않는다.
* 처음 여는 폴더에서 agent가 신뢰 확인 같은 시작 화면에 멈추면 native session이 아직 없어
  binding이 발급되지 않는다(2026-09-29 격리 실측). 모바일 Terminal 입력은 fail closed하므로 확인은
  PC의 Herdr에서 한다.
* 실행 중 agent의 폴더 이동, 모바일에서 workspace·tab·pane 닫기는 제공하지 않는다.

기각한 대안:

- Bridge가 agent process를 직접 실행: ADR-001의 자체 runner다.
- shell pane에 실행 명령을 raw input으로 입력: 준비 상태 확인이 없고, 미지원 API를 raw pane input으로
  우회하지 않는다는 원칙에 어긋난다.
- 임의 경로 입력이나 host filesystem 탐색: 경로 검증이 공격면이 되고 신뢰 경계가 사라진다.
- agent 인자 허용: resume과 permission 우회가 가능해진다.

---

## ADR-038 — Codex Chat은 Herdr가 보고한 native session으로 read-only 표시한다

### Status

Accepted (2026-10-05). [P1-02](https://github.com/psw7205/herdr-remote/issues/5). Codex 입력은
[P1-01](https://github.com/psw7205/herdr-remote/issues/4)에서 다룬다.

### Context

Codex CLI rollout의 구조와 Herdr Codex hook의 `agent_session` 보고는
[2026-09-24 격리 Herdr Codex CLI 조사](records/integration-findings.md#2026-09-24--격리-herdr-codex-cli-조사)에서
확인했다. 조건부 입력 patch의 `agent.binding`은 Claude process의 PID별 native metadata로 binding을
만든다. Codex hook이 보고한 session ID가 현재 foreground process incarnation과 결합돼 있는지는
검증하지 않았으므로, Codex에는 ADR-034의 write 조건을 만족하는 binding이 없다. Transcript 읽기는
write와 분리된 경로라(ADR-004) binding 없이도 열 수 있다.

### Decision

**Identity.** Codex pane의 native session은 Herdr의 `agent_session`(agent `codex`, kind `id`)이
보고한 값만 쓴다. Bridge는 process 목록, cwd, 파일 수정 시각으로 추정하지 않는다. 보고된 ID의
rollout을 찾으면 item ID는 `codex:<native-id>`다. 보고가 없거나 rollout이 아직 없으면 status만 있는
`pane:<pane-id>` item이고, 이후 같은 pane에 `codex:` item이 새로 생기면 `pane:` item은 그 item으로
supersede된다. 한 Refresh에서 두 pane이 같은 ID를 보고하면 어느 쪽도 `codex:` item을 새로 차지하지 않는다.

**Binding 없음.** Codex pane에는 `agent.binding`을 호출하지 않는다. 그 결과가 server 단위 조건부 입력
판정(`herdr.conditional_input`)에 쓰이므로 Codex 오류가 Claude 입력 가능 여부를 바꾸면 안 된다.
`codex:` item에는 `runtime_binding`과 Terminal이 없고 lifecycle은 `unbound`다. prompt, interrupt,
Terminal read/input은 모두 fail closed한다. `unbound`의 뜻은 "검증된 binding을 가진 적이 없는 item"으로
넓힌다. `pane:` item과 `codex:` item이 여기에 해당한다.

**Lifecycle.** `codex:` item은 pane이 snapshot에서 사라지거나, Herdr가 그 pane에서 codex를 더 이상
보고하지 않거나, `agent_session`이 다른 native ID를 가리킬 때 `ended`가 된다. 보고가 비어 있는 동안은
같은 pane의 item을 유지한다.

**Rollout 찾기.** `<codex-dir>/sessions/*/*/*/rollout-*-<native-id>.jsonl`이 정확히 하나이고 첫 줄이
같은 ID의 `session_meta`일 때만 Chat을 연다. `archived_sessions/`는 찾지 않는다. 실행 중인 session은
`sessions/` 아래에 쓰며, archive의 사본을 붙이면 지난 대화를 현재 session으로 보일 수 있다.
Codex home은 Bridge `-codex-dir`(기본 `~/.codex`)이다.

**Projection.** rollout은 parent tree가 없는 선형 기록이다. `response_item` 중 `message`만 Chat으로
옮긴다. user는 `internal_chat_message_metadata_passthrough.content_item_kinds`에서 같은 위치의 kind가
`user.text`인 `input_text`만 표시한다. AGENTS.md와 환경 정보 같은 주입 context도 role `user`로 기록되기
때문이다. kinds가 없거나 content와 길이가 다르면 그 user record는 숨긴다. assistant는 `output_text`를
kinds와 무관하게 표시한다(관찰된 kind는 `unknown`). developer, reasoning, tool call과 결과, `event_msg`,
알 수 없는 type은 표시하지 않는다. `event_msg`의 `item_completed`·`task_complete`는 같은 message를
반복하므로 함께 쓰면 중복된다. message ID는 Codex가 모든 record에 쓰는 `ordinal`이다. 같은 ordinal이
다른 내용으로 다시 나오거나 다른 session의 `session_meta`가 나오면 transcript를 invalid로 표시한다.

### Consequences

* Codex session을 mobile에서 읽을 수 있다. 입력과 Terminal은 PC의 Herdr에서 한다.
* Herdr의 Codex integration hook이 설치돼 있어야 `agent_session`이 보고된다(`herdr integration status`로
  확인). hook은 Codex의 trust review를 거쳐야 동작하고, 설치 전에 시작한 session의 SessionStart는
  소급해 보고되지 않을 수 있다. 보고가 없으면 Codex pane은 status만 보인다.
* `content_item_kinds`가 없는 이전 Codex version의 rollout은 사용자 입력이 Chat에 보이지 않고 assistant
  응답만 보인다.
* hook 보고가 빈 사이 같은 pane의 Codex가 다른 session으로 바뀌면 이전 rollout을 read-only로 계속 보일
  수 있다. 입력 경로가 없으므로 다른 process에 쓰지는 않는다.
* Codex write(prompt, interrupt, Terminal)는 Herdr가 Codex process incarnation에 결합한 binding을
  발급하고 그 경계를 검증한 뒤 연다([P1-01](https://github.com/psw7205/herdr-remote/issues/4)).
  새 session 시작의 kind는 여전히 `claude`뿐이다(ADR-037).

기각한 대안:

- process 목록, `lsof`, cwd, 최근 수정 시각으로 rollout 추정: 같은 cwd의 session을 구별하지 못하고
  identity 판단을 Bridge heuristic에 넘긴다.
- Codex pane에도 `agent.binding` 호출: Codex 실패가 server 단위 capability 판정에 섞여 Claude 입력을
  막을 수 있다.
- `event_msg`로 Chat 구성: `response_item`과 같은 message를 반복하므로 둘을 합치면 중복된다.
- 읽은 줄 번호를 message ID로 사용: reader가 어디서부터 읽었는지에 따라 달라진다.

---

## ADR-039 — Tool 호출은 Chat item으로 투영하고 결과는 message 갱신으로 보낸다

### Status

Accepted (2026-10-05). [P2-01](https://github.com/psw7205/herdr-remote/issues/14). Claude만 해당한다.

### Context

Chat은 text record만 보여 agent가 tool을 실행하는 동안 무엇을 하는지 mobile에서 알 수 없었다.
[2026-10-05 실측](records/integration-findings.md#2026-10-05--claude-transcript-live-기록-단위와-tool-식별자)에서
Claude Code는 `tool_use` block을 실행 시작 때, 짝이 되는 user record의 `tool_result`(같은 `tool_use_id`)를
실행이 끝날 때 쓴다. 병렬 tool은 같은 `message.id` 안에서 `tool_use`(A) → `tool_result`(A) → `tool_use`(B)
순으로 섞이고 parent chain은 한 줄이다. result가 끝내 없는 `tool_use`도 있다.

기존 stream은 뒤에 추가된 message만 Commit하고, 앞부분이 바뀌면 `Reset`으로 새 epoch snapshot을 보낸다.
tool 결과가 도착할 때마다 앞의 item이 바뀌므로 그대로 두면 tool 하나마다 epoch가 바뀐다.

### Decision

**Projection.** `internal/claude`가 현재 branch(최신 leaf의 parent chain)의 `tool_use`마다 role `tool` item을
transcript 순서대로 만든다. 같은 chain의 `tool_result`만 짝짓고, 짝이 없는 `tool_result`, `server_tool_use`처럼
user `tool_result`와 짝을 이루지 않는 block은 표시하지 않는다. text message의 ID와 순서는 바뀌지 않는다.
item ID는 `<record uuid>/<tool_use id>`다.

**Item.** `Message`에 `tool` object를 더한다. `id`(tool_use id), `name`, `summary`, `state`, `input`,
`input_truncated`, `result`, `result_truncated`다. user·assistant message에는 `tool`이 없어 wire 형태가 그대로다.
`summary`는 Bridge가 잘 알려진 tool에서 한 줄로 만든다(Bash command 첫 줄, Read·Write·Edit 경로를 record `cwd`
기준 상대 경로로, Grep·Glob pattern, Task·Agent description, WebFetch host, WebSearch·ToolSearch query, Skill 이름, TodoWrite 개수).
그 밖의 tool은 빈 값이고 client는 이름만 보인다. `input`은 들여쓴 JSON, `result`는 text block을 이은 text다.
image 등 text가 아닌 block은 `[image]`처럼 이름만 남기고 내용(base64)은 복사하지 않는다. 둘 다 8 KiB에서
UTF-8 경계로 자르고 `*_truncated`를 켠다.

**State.** `running`은 result가 아직 없는 호출이다. 결과가 있으면 `completed`, `is_error`가 참이면 `error`다.
result 없이 chain이 넘어가면 `unknown`이다. 다른 `message.id`의 assistant record나 사용자 prompt(중단 안내
포함)가 뒤에 오면 넘어간 것으로 본다. Claude Code는 모든 result를 쓴 뒤에야 다음 API message나 prompt로 가므로
그때까지 없는 result는 오지 않는다. session 종료처럼 transcript가 더 바뀌지 않는 경우는 Bridge가 판정하지
않는다. client가 `lifecycle`이 `ended`·`superseded`이거나 `status`가 `working`·`needs_attention`이 아닐 때
`running`을 결과 없음으로 보인다. 이 정보는 이미 `Meta`와 `agent.status`에 있다.

**Protocol.** 새 tool item은 `message.tool`, 기존 tool item의 상태·결과 변화는 `message.updated`(payload는 바뀐
item 전체)로 보낸다. 둘 다 snapshot 갱신과 같은 `Commit`에서 sequence를 받으므로 cursor·replay·live의 원자성은
그대로다. 한 batch의 갱신은 추가보다 먼저 나간다. 앞부분의 text message가 바뀌거나, item이 줄거나, 같은 자리의
ID가 다르면 이전처럼 `Reset`이다. client는 모르는 role과 `tool` 없는 tool item을 건너뛰고, 갖지 않은 ID의
`message.updated`는 버린다(다음 snapshot이 맞춘다). 목록 preview(`last_message`)는 tool item을 건너뛴 최신
text message다.

**Fail soft.** tool block의 field는 관대하게 읽는다. `id`·`name`이 문자열이 아닌 `tool_use`, `tool_use_id`가
없는 `tool_result`는 버리고, 형식이 다른 `input`·`content`·`is_error`는 빈 값이나 거짓으로 둔다. tool block 때문에
transcript가 invalid가 되지 않는다.

### Consequences

* Chat에서 tool 진행과 결과를 compact group으로 보고, 행을 눌러 입력과 결과를 펼친다.
* tool 결과마다 epoch가 바뀌지 않아 재연결 replay가 그대로 동작한다.
* snapshot이 tool마다 최대 16 KiB를 더 싣는다. 긴 session의 snapshot과 memory가 커지며, 상한(8 KiB)은 그
  비용과 mobile에서 확인할 만한 양 사이의 선택이다. 전체 내용은 Terminal이나 PC에서 본다.
* Codex rollout의 tool 기록은 표시하지 않는다.
* `summary`의 경로 상대화는 record `cwd` 기준이라, session 도중 디렉터리를 옮기면 같은 project의 파일도 절대
  경로로 보일 수 있다.

기각한 대안:

- `tool.started`·`tool.completed`·`tool.failed` 별도 event: 같은 item의 상태를 세 이름으로 나누면 처음부터 끝난
  상태로 발견된 호출(재연결, 한 batch에 use와 result)을 다시 started부터 흉내 내야 한다. item 하나와 갱신 하나가
  snapshot과 같은 모양이다.
- result 도착마다 `Reset`: tool마다 새 epoch와 전체 snapshot을 보내 mobile 재전송이 커지고 replay가 의미를 잃는다.
- 상세를 별도 HTTP endpoint로 지연 조회: snapshot은 작아지지만 Bridge가 transcript를 다시 찾거나 별도 cache를
  가져야 하고 route·Origin 검사가 늘어난다. 상한으로 크기를 먼저 제한한다.
