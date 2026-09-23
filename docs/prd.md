# Herdr Mobile Chat — Product Requirements Document

**Status:** Draft v0.1
**Product type:** Self-hosted mobile-first Herdr client
**Primary client:** Web / PWA
**Network scope:** Private Tailnet only
**First vertical slice:** Claude Code (integration gate 통과 후)
**Follow-up agent:** Codex
**Target user:** Single user / developer

---

# 1. Overview

Herdr Mobile Chat은 Herdr에서 이미 실행 중인 AI coding agent를 모바일에서 자연스럽게 확인하고 대화하기 위한 **chat-first remote client**다.

기존 Herdr workflow를 대체하지 않는다.

Herdr는 계속 다음을 담당한다.

* workspace / tab / pane lifecycle
* terminal / PTY
* agent process
* agent status
* native agent session association

본 프로젝트는 그 위에 모바일에 적합한 별도의 presentation layer를 제공한다.

핵심 개념은 다음과 같다.

> 하나의 agent session을 Terminal과 Chat이라는 두 개의 View로 표현한다.

사용자는 PC에서 Herdr terminal을 사용하다가 모바일에서 같은 세션을 열고 대화를 이어갈 수 있어야 한다.

모바일에서 생성되는 별도의 AI conversation이나 별도의 agent process는 존재하지 않는다.

---

# 2. Problem

Herdr는 여러 coding agent를 동시에 실행하고 관리하기 좋은 환경이지만, 모바일에서 agent와 상호작용하기에는 terminal UI의 제약이 크다.

특히 다음 작업의 모바일 경험이 좋지 않다.

* 긴 agent 응답 읽기
* Markdown / code 확인
* 여러 실행 중 agent 중 입력이 필요한 agent 파악
* 간단한 follow-up prompt 입력
* agent 질문이나 permission 요청 응답
* 작업 완료 여부 확인
* 변경 파일 / diff 확인
* 화면 잠금이나 앱 전환 이후 세션 복귀

기존 remote terminal이나 Herdr Web UI는 terminal을 모바일에 옮기는 데 집중하는 경우가 많다.

하지만 이 프로젝트가 해결하려는 문제는:

> "모바일에서도 terminal을 사용할 수 있게 한다."

가 아니라:

> "이미 Herdr에서 실행 중인 agent와 모바일 메신저처럼 자연스럽게 상호작용한다."

이다.

---

# 3. Product Principles

## 3.1 Chat first

기본 interaction surface는 terminal이 아니라 conversation이다.

Terminal은 모든 interaction의 fallback이며 primary UI가 아니다.

---

## 3.2 Existing session first

새로운 agent process나 conversation을 생성하지 않는다.

가능한 경우 항상 현재 Herdr pane에서 실행 중인 agent session을 그대로 사용한다.

```text
Herdr Pane
    │
    └── Existing Claude / Codex Process
             │
             ├── Terminal View
             │
             └── Chat View
```

두 View 모두 같은 process와 같은 native agent session을 바라본다.

---

## 3.3 Projection, not duplication

Chat UI를 위해 별도의 conversation database를 source of truth로 만들지 않는다.

대신 기존 데이터를 목적에 따라 사용한다.

| Data                               | Source of truth         |
| ---------------------------------- | ----------------------- |
| workspace / pane lifecycle         | Herdr                   |
| agent state                        | Herdr                   |
| agent process                      | Herdr / PTY             |
| conversation history               | Native agent transcript |
| current interactive terminal state | PTY                     |
| mobile presentation state          | Herdr Mobile Chat       |
| unread / UI preferences            | Herdr Mobile Chat       |

Chat은 원본 session의 **projection/read model**이다.

---

## 3.4 PTY remains the interactive truth

Transcript가 agent의 모든 interactive state를 표현한다고 가정하지 않는다.

예를 들어 CLI가 terminal에서 다음과 같은 UI를 표시할 수 있다.

```text
Select an option:

> Allow once
  Allow always
  Deny
```

이 상태가 transcript에서 안정적으로 구조화되어 있지 않다면 Chat UI가 억지로 해석하지 않는다.

그 경우 사용자에게 Terminal View를 제공한다.

---

## 3.5 Mobile lifecycle is normal behavior

다음 상황은 오류가 아니라 정상적인 사용 흐름으로 간주한다.

```text
Chat
 → screen off
 → WebSocket disconnected
 → agent continues working
 → screen on
 → reconnect
 → missing events restored
```

따라서:

```text
WebSocket lifetime != Agent session lifetime
```

이어야 한다.

---

# 4. Goals

MVP의 목표는 다음 사용자 흐름을 안정적으로 지원하는 것이다.

### G1. 현재 실행 중 agent 확인

사용자는 모바일 홈 화면에서 현재 Herdr에서 실행 중인 agent들을 확인할 수 있다.

각 agent에는 최소한 다음 상태가 보인다.

* agent 종류
* workspace
* `needs_attention` / `working` / `idle` / `completed` / `error` 상태
* 마지막 activity
* attention 필요 여부

---

### G2. 기존 agent conversation 확인

사용자는 실행 중인 Claude Code 또는 Codex 세션의 기존 대화를 모바일에 적합한 형태로 읽을 수 있다.

표현 대상은 최소한 다음과 같다.

* user message
* assistant message
* Markdown
* code block
* tool activity
* errors
* system/status events

---

### G3. 기존 agent와 계속 대화

모바일 composer에서 메시지를 보내면 별도 agent를 실행하지 않는다.

입력은 기존 Herdr pane / PTY로 전달된다.

```text
Mobile composer
       ↓
Herdr Bridge
       ↓
Existing Herdr Pane
       ↓
Existing Claude / Codex Process
```

PC terminal에서 직접 입력하는 것과 동일한 session context를 유지해야 한다.

---

### G4. 연결이 끊겨도 agent 작업 유지

브라우저가 background 상태가 되거나 WebSocket 연결이 끊겨도 agent process는 영향을 받지 않는다.

재접속 시 현재 session과 conversation 상태를 복원한다.

---

### G5. Attention 중심 UX

사용자는 여러 세션을 일일이 열어보지 않고도 어느 agent가 자신의 입력을 기다리고 있는지 파악할 수 있다.

---

### G6. Terminal fallback

Chat UI에서 처리할 수 없는 interaction이 존재할 경우 같은 pane의 Terminal View로 즉시 전환할 수 있다.

---

# 5. Non-Goals

MVP에서는 다음 기능을 구현하지 않는다.

### Full IDE

프로젝트 전체를 모바일 IDE로 만들지 않는다.

다음은 MVP 범위 밖이다.

* full source editor
* Monaco 기반 IDE
* language server
* debugger
* Git commit UI
* merge UI
* full repository management

---

### Agent runtime replacement

Claude Code, Codex 또는 Herdr를 자체 runtime으로 대체하지 않는다.

다음 구조를 만들지 않는다.

```text
Mobile App
    ↓
Our custom agent runner
    ↓
Claude / Codex
```

Herdr가 runtime owner다.

---

### Universal agent support

초기부터 모든 CLI agent를 지원하지 않는다.

MVP 지원 대상:

```text
Claude Code
Codex
```

다른 agent는 adapter 확장으로 추후 지원한다.

---

### Public Internet access

공개 SaaS 형태의 인증/계정 시스템을 만들지 않는다.

서비스는 Tailnet 내부에서 사용하는 것을 기본 전제로 한다.

---

### Multi-user collaboration

MVP는 single-user product다.

사용자 초대, role, organization, shared workspace 등의 개념을 도입하지 않는다.

---

### Multi-host

MVP는 하나의 Herdr host를 대상으로 한다.

다중 Herdr 서버 관리 기능은 추후 검토한다.

---

# 6. Primary User Journey

대표적인 사용자 흐름은 다음과 같다.

```text
PC
│
│ Herdr에서 Codex 실행
│ prompt 입력
│
▼
Codex 작업 중
│
│ 사용자는 자리에서 이동
│
▼
Mobile
│
│ PWA 실행
│
▼
Needs Attention / Active Agents
│
▼
Codex session 선택
│
▼
기존 대화 확인
│
│ "이 부분 테스트까지 해줘"
│
▼
같은 Codex PTY에 prompt 전달
│
▼
Codex 계속 작업
│
│ 화면 잠금
│
▼
background
│
│ Codex 작업 완료
│
▼
notification (후속 optional)
│
▼
앱 재진입
│
▼
완료된 응답 / 변경사항 확인
```

---

# 7. Information Architecture

전체 화면 구조는 다음과 같다. 첫 slice는 Sessions, Chat, Terminal에 한정하고
Changes와 확장 Settings는 후속 단계다.

```text
/
├── Sessions
│
├── Session
│   ├── Chat
│   ├── Changes
│   └── Terminal
│
└── Settings
```

## 7.1 Sessions

앱 진입 시 기본 화면이다.

세션을 상태 중심으로 표현한다.

권장 그룹:

```text
Needs Attention

Working

Recent
```

각 session row는 최소한 다음 정보를 표시한다.

| Field           | Example                  |
| --------------- | ------------------------ |
| agent           | Codex                    |
| project         | my-app            |
| status          | Working                  |
| latest activity | 12 sec ago               |
| attention       | Permission required      |
| last message    | "Tests passed except..." |

Workspace / tab / pane 같은 Herdr 내부 개념은 필요한 경우 secondary metadata로만 노출한다.

---

# 8. Session Model

사용자가 보는 하나의 채팅방은 **하나의 native agent session**에 대응한다.

개념 모델:

```text
Herdr Workspace
    ↓
Herdr Tab
    ↓
Herdr Pane
    ↓
Agent Process
    ↓
Native Agent Session
    ↓
Conversation
```

사용자에게는 마지막 두 개를 하나의 Session으로 표현한다.

예:

```text
Codex
my-app
feature/mobile-chat
```

내부적으로는 다음 identity를 유지할 수 있다.

```text
host
workspace_id
tab_id
pane_id
agent_type
agent_session_id
```

`agent_session_id`를 사용할 수 있는 경우 transcript 탐색의 primary key로 사용한다.

cwd/mtime 기반 transcript guessing은 첫 slice에서 사용하지 않는다.

모든 write는 Herdr가 발급한 현재 `runtime_binding`을 조건으로 실행한다.
`pane_id`, `terminal_id`, 일반 `revision`은 process/session incarnation을 대신하지 않는다.
identity가 불명확하거나 조건부 입력을 지원하지 않으면 prompt/interrupt/raw input을 비활성화한다.

목록은 현재 Herdr에서 발견한 active agent를 대상으로 한다. 열어 둔 session의 process가
종료되면 `ended/unavailable`로 표시하고 입력과 Terminal 제어를 비활성화한다.
`completed`는 turn 완료 상태이며 살아 있는 process에서 후속 prompt가 가능하다.
historical transcript만 남은 session을 탐색하는 기능은 첫 slice에서 제외한다.

---

# 9. Data Ownership

## Herdr

다음을 소유한다.

```text
workspace
tab
pane
PTY
agent process
agent lifecycle
agent status
native agent session association
```

---

## Claude / Codex

다음을 소유한다.

```text
conversation history
assistant messages
tool execution records
native session metadata
```

가능한 경우 agent가 생성하는 native transcript를 그대로 이용한다.

---

## Herdr Mobile Chat

다음 정보만 자체 관리한다.

```text
last read position
event cursor
UI state
notification state
favorites
local preferences
```

conversation 전체를 별도 DB에 복제하는 것은 기본 설계가 아니다.

필요하다면 성능 최적화를 위한 cache/index는 허용하지만 source of truth가 되지는 않는다.

---

# 10. Agent Adapter

Agent마다 transcript 형식과 interaction 방식이 다르기 때문에 adapter 계층을 둔다.

개념적으로 다음 interface를 만족한다.

```text
AgentAdapter

identifySession()
readHistory()
watchChanges()
normalizeEvents()
sendPrompt()
interrupt()
getCapabilities()
```

초기 구현:

```text
ClaudeCodeAdapter
CodexAdapter
```

Adapter는 agent native event를 공통 domain event로 변환한다.

---

# 11. Unified Conversation Model

클라이언트가 Claude/Codex 개별 transcript format을 알아서는 안 된다.

Bridge에서 다음과 같은 semantic event로 normalize한다.

| Event                     | 의미                |
| ------------------------- | ----------------- |
| `message.user`            | 사용자 입력            |
| `message.assistant`       | assistant message |
| `message.assistant.delta` | streaming update  |
| `tool.started`            | tool 실행 시작        |
| `tool.completed`          | tool 실행 완료        |
| `tool.failed`             | tool 실패           |
| `permission.requested`    | permission 필요     |
| `question.requested`      | user input 필요     |
| `agent.status`            | agent 상태 변경       |
| `session.completed`       | 작업 완료             |
| `session.error`           | session 오류        |

초기 단계에서는 transcript에서 안정적으로 식별할 수 있는 event만 structured event로 노출한다.

불확실한 terminal interaction을 추측하여 structured event로 변환하지 않는다.

---

# 12. Read Path

Chat history는 다음 경로로 생성된다.

```text
Herdr
  │
  └ agent_session_id
         │
         ▼
Agent Adapter
         │
         ▼
Native Transcript
         │
         ▼
Incremental parser
         │
         ▼
Normalized Events
         │
         ▼
Chat View
```

요구사항:

* 전체 transcript를 매번 다시 parse하지 않는다.
* incremental read를 지원한다.
* agent process와 transcript writer 간의 partial write를 안전하게 처리한다.
* 동일 event가 중복 표시되지 않는다.
* client reconnect와 transcript polling/watch가 독립적이어야 한다.

---

# 13. Write Path

사용자 메시지는 transcript에 직접 쓰지 않는다.

항상 실행 중인 Herdr pane으로 전달한다.

```text
Chat Composer
      ↓
Bridge
      ↓
Herdr pane input
      ↓
Existing Agent Process
```

이를 통해:

* terminal과 mobile input이 같은 process를 사용하고
* agent context가 유지되며
* CLI 자체의 state machine을 우회하지 않는다.

Command는 `command_id`, `session_id`, `runtime_binding`, `command_type`, `payload`를 가진다.
결과는 `accepted`, `rejected`, `delivery_unknown`으로 구분한다. 같은 ID의 retry는
같은 결과를 반환하며 payload/target이 다르면 거부한다. 전송 전에 durable receipt를 기록하고
timeout/crash 뒤 불확실한 command를 자동 재전송하지 않는다. receipt는 conversation DB가 아니다.
Client는 응답을 받기 전 브라우저가 reload되더라도 같은 초안과 binding에 같은
`command_id`를 유지한다. 전달 여부가 불확실할 때 내용이나 binding이 바뀌면
이전 command를 확인하거나 명시적으로 포기하기 전에는 새 ID로 보내지 않는다.

Chat의 user message는 native transcript에서 관찰한 뒤 확정한다. 전송 중 표시는 별도의 pending UI다.
Herdr 조건부 write가 없으면 조회 후 입력하는 workaround로 이 보장을 대체하지 않는다.

---

# 14. Chat UI

Session 기본 화면이다.

구성:

```text
Header
│
├ Agent
├ Project
├ Status
└ Terminal button

Conversation
│
├ User Message
├ Assistant Message
├ Tool Card
├ Status Event
├ Permission Card
└ Error

Composer
│
├ Attachment
├ Textarea
├ Stop
└ Send
```

---

# 15. Message Rendering

## Assistant Message

지원:

* Markdown
* headings
* lists
* tables
* inline code
* fenced code blocks
* links
* copy
* text selection

Code block에는 모바일에서 쉽게 사용할 수 있는 copy action을 제공한다.

---

## Tool Card

tool call은 기본적으로 compact하게 표시한다.

예:

```text
✓ Read
  src/api/users.ts
```

또는:

```text
✓ Bash
  pnpm test

  142 tests passed
```

사용자가 필요할 때 상세 내용을 펼칠 수 있다.

---

## Status Event

상태 변화는 conversation을 방해하지 않도록 작게 표현한다.

예:

```text
Codex started working

Compacting context...

Waiting for input
```

---

# 16. Composer

모바일에서 가장 많이 사용하는 control이다.

필수 요구사항:

* multiline input
* mobile IME 정상 동작
* draft 유지
* send
* stop / interrupt
* keyboard safe area 대응
* 긴 prompt 작성 가능

후속 optional attachment 지원 (첫 slice 및 핵심 MVP 필수 조건에서 제외):

* image
* 일반 file

첨부가 agent CLI에서 직접 지원되지 않는 경우 Bridge가 host filesystem에 저장하고 접근 가능한 path를 prompt에 전달하는 방식 등을 adapter가 결정한다.

---

# 17. Attention Model

Session에는 presentation용 status를 계산한다.

권장 상태:

```text
needs_attention
working
idle
completed
error
```

`needs_attention`이 최우선이다.

예:

```text
permission required
question waiting
agent explicitly waiting for user
```

상태 판별은 가능한 한 Herdr agent status와 structured transcript event를 조합한다.

터미널 문자열을 임의 정규식으로 분석해 상태를 추측하는 것은 최소화한다.

---

# 18. Permission / Question Handling

Agent Adapter가 충분히 구조화된 정보를 제공할 수 있을 경우 Chat에서 native card로 표현한다.

예:

```text
Permission required

Run:
rm -rf dist

[Allow once]
[Deny]
```

또는:

```text
Which implementation should I use?

○ WebSocket
○ SSE
○ Polling
```

다만 structured response가 안전하지 않거나 agent CLI 내부 state와 정확히 연결할 수 없는 경우:

```text
Agent requires terminal interaction

[Open Terminal]
```

을 표시한다.

정확하지 않은 자동화를 제공하는 것보다 terminal fallback을 우선한다.

---

# 19. Terminal View

Terminal은 동일 Herdr pane의 실제 PTY를 보여준다.

목적:

* Chat projection으로 표현할 수 없는 interaction
* CLI picker
* unusual prompts
* debugging
* emergency fallback

필요 기능:

* terminal rendering
* desktop/Herdr의 PTY 크기를 유지하는 mirror
* scroll
* text input
* Esc
* Tab
* Ctrl-C
* arrow keys
* Enter

Terminal을 full IDE 수준으로 확장하지 않는다.

Terminal observer는 기존 session만 관찰하며 resume/start/resize/takeover를 하지 않는다.
raw input에도 동일한 runtime binding 보호를 적용한다. observer disconnect는 agent lifecycle을 바꾸지 않는다.
현재 Herdr direct attach는 이 조건을 충족하지 않으므로 그대로 proxy하지 않는다.

---

# 20. Changes View

Git working tree의 변경 파일을 모바일에서 읽는 후속 optional 기능이다.
사용자 변경이 섞일 수 있으므로 특정 agent의 변경이라고 단정하지 않는다.
첫 slice 및 핵심 MVP 필수 조건에서 제외한다.

후속 요구사항:

```text
changed files
diff
added / deleted line count
```

파일 수정 기능은 제공하지 않는다.

목적은:

> "무엇을 바꿨는가?"

를 빠르게 확인하는 것이다.

Git workflow를 모바일에서 완성하는 것이 목적이 아니다.

---

# 21. Connectivity

기본 배포 모델:

```text
Herdr Host
     │
Bridge
127.0.0.1:<port>
     │
tailscale serve
     │
Tailnet HTTPS
     │
Mobile PWA
```

Bridge를 public interface에 직접 bind하지 않는 구성을 기본으로 한다.

외부 인터넷 공개는 지원 대상이 아니다.

---

# 22. Authentication

MVP에는 별도의 회원가입 / login system을 도입하지 않는다.

Tailnet network boundary + 최소 권한 ACL + browser Origin 검증을 security boundary로 사용한다.
ACL이 넓은 배포에서도 Tailscale Serve의 사용자 identity header를 Bridge에서
소유자 로그인과 대조하여 다른 tailnet member의 읽기/제어를 모두 거부한다.
Bridge는 localhost에 bind하고 외부 노출은 Tailscale Serve로 한정한다.
HTTP mutation과 WebSocket handshake에서 명시적인 exact Origin allowlist를 검증하며
wildcard CORS, null Origin, 임의 Host를 허용하지 않는다. mutation에는 JSON 요청을 요구한다.

필요할 경우 추가적인 lightweight application lock을 추후 제공할 수 있다.

다만 자체:

```text
users table
password
OAuth
JWT refresh token
organization
role
```

등의 인증 시스템은 구축하지 않는다.

---

# 23. Reconnection Model

Client connection은 disposable하다.

각 connection은 마지막으로 처리한 event cursor를 유지한다.

개념 흐름:

```text
Client
   │
   ├ lastEventId = 152
   │
   X disconnected
   │
   │ agent continues
   │
   ▼
Reconnect
   │
   └ lastEventId = 152
         ↓
Bridge
         ↓
153...current replay
```

Cursor는 `{epoch, sequence}`다. snapshot은 cursor C와 상태를 함께 반환한다.
`subscribe(after=C)`는 같은 session lock에서 C 이후 replay를 확보하고 live subscriber를 등록한다.
subscriber queue overflow는 drop 대신 재동기화를 요구한다.
Bridge restart, transcript 교체, 복구 불가능한 resync는 epoch를 바꾼다.
다른 epoch 또는 replay buffer 범위 밖 cursor에는 fresh snapshot을 반환한다.

중요한 요구사항은:

> reconnect 후 사용자가 conversation의 중간 상태를 잃지 않아야 한다.

이다.

---

# 24. Background Behavior

PWA가 background로 이동했을 때 client connection이 유지된다고 가정하지 않는다.

Agent 작업은 server-side에서 계속된다.

foreground 복귀 시 다음을 수행한다.

```text
reconnect
refresh agent status
resume transcript
restore missing messages
update attention state
```

---

# 25. Notifications

Notification은 후속 optional 기능이며 첫 slice 및 핵심 MVP 필수 조건에서 제외한다.
구현 시 대상은 두 종류로 제한한다.

### Input required

```text
Codex needs your input
my-app
```

### Task completed

```text
Claude finished
my-app
```

notification을 누르면 해당 session으로 이동한다.

Web Push가 플랫폼 제약으로 충분하지 않은 경우 네이티브 클라이언트 전환 판단의 주요 근거로 사용한다.

---

# 26. PWA Requirements

초기 client는 mobile-first Web/PWA다.

필수:

* standalone display
* responsive mobile layout
* home screen install
* safe-area 대응
* keyboard viewport 대응
* touch-first interaction
* background/foreground recovery

Desktop 지원은 가능하지만 desktop-first layout은 별도로 최적화하지 않는다.

---

# 27. Bridge Responsibilities

Bridge는 프로젝트의 핵심 backend component다.

책임:

```text
Herdr connection
session discovery
agent adapter management
transcript watching
event normalization
WebSocket/API
prompt forwarding
terminal proxy
attachment handling
reconnect support
notification trigger
```

Bridge가 해야 하지 않는 것:

```text
run its own AI agents
own conversation context
modify agent transcript
replace Herdr lifecycle
become an IDE backend
```

---

# 28. Client Responsibilities

Client는 thin presentation layer를 지향한다.

책임:

```text
session list
conversation rendering
composer
tool cards
attention UX
terminal UI
diff viewer
connection status
local presentation preferences
```

Agent별 transcript logic을 client에 넣지 않는다.

---

# 29. Error Handling

사용자에게 internal adapter 오류를 그대로 노출하지 않는다.

주요 오류 상태:

### Herdr unavailable

```text
Herdr is unavailable.
Retrying...
```

### Agent session unknown

```text
Agent is running, but its conversation could not be identified.

[Open Terminal]
```

### Transcript unsupported

```text
Chat view isn't available for this session.

[Open Terminal]
```

### Connection lost

```text
Connection lost.
Agent continues running.
```

Client connection loss가 agent process loss처럼 보이면 안 된다.

---

# 30. MVP Scope

아래 표는 핵심 MVP와 후속 optional capability를 구분한다.
첫 구현은 Claude 하나의 discovery → transcript Chat → 동일 PTY prompt → transcript 응답이며,
interrupt/reconnect/동일 Terminal fallback을 포함한다. Herdr integration gate를 통과하기 전에는
write-enabled UI를 구현 완료로 간주하지 않는다. 세부 gate와 미지원 사항은
`docs/integration-findings.md`, 실행 순서는 `docs/implementation-plan.md`에 기록한다.

| Area                         | MVP   |
| ---------------------------- | ----- |
| Herdr integration            | Yes   |
| Single host                  | Yes   |
| Claude Code                  | Yes   |
| Codex                        | 후속 adapter |
| Running session discovery    | Yes   |
| Session list                 | Yes   |
| Attention status             | Yes   |
| Transcript chat              | Yes   |
| Markdown / code              | Yes   |
| Tool cards                   | 후속 optional |
| Streaming / live updates     | Yes   |
| Send prompt to existing pane | Yes   |
| Interrupt                    | Yes   |
| Reconnect                    | Yes   |
| Terminal fallback            | Yes   |
| File attachment | 후속 optional |
| Changed files | 후속 optional |
| Diff viewer | 후속 optional |
| PWA                          | Yes   |
| Tailscale deployment         | Yes   |
| Completion notification | 후속 optional |
| Input-required notification | 후속 optional |

---

# 31. Post-MVP Candidates

다음은 MVP 완료 후 실제 사용 과정에서 필요성이 확인될 때만 추가한다.

```text
native iOS client
native Android client

multiple Herdr hosts

session search
conversation full-text search

voice input
share sheet integration

favorites / pinning

image preview improvements

rich approval support

additional agents
  Gemini CLI
  OpenCode
  others

native push infrastructure

session analytics
```

---

# 32. Explicitly Deferred

다음 기능은 요구가 생기더라도 바로 추가하지 않는다.

```text
mobile source editor
Git commit/push UI
PR management
browser IDE
multi-user
cloud hosting
public authentication
plugin marketplace
agent orchestration engine
agent-to-agent workflow
```

제품이 다시 generic AI IDE로 확장되는 것을 방지하기 위한 경계다.

---

# 33. Success Criteria

MVP는 다음 조건을 만족하면 성공으로 판단한다.

## Session continuity

PC에서 실행한 Claude/Codex 세션을 모바일에서 별도 설정 없이 찾아볼 수 있다.

---

## Conversation continuity

PC terminal에서 입력한 대화와 모바일에서 입력한 대화가 동일 conversation에 나타난다.

---

## No duplicate agent

모바일에서 prompt를 보내더라도 새로운 Claude/Codex process 또는 session이 생성되지 않는다.

---

## Mobile usability

대부분의 일반적인 follow-up interaction은 Terminal View를 열지 않고 Chat View에서 완료할 수 있다.

---

## Recovery

모바일 화면을 잠갔다가 다시 열었을 때 agent 작업과 conversation이 정상 복구된다.

---

## Attention

agent가 사용자 입력을 필요로 할 경우 Session 목록에서 이를 쉽게 식별할 수 있다.

---

## Terminal escape hatch

Chat parser가 처리하지 못하는 CLI 상태에서도 사용자는 기존 pane을 직접 제어할 수 있다.

---

## Private access

Tailnet 밖에서는 기본적으로 서비스에 접근할 수 없다.

---

# 34. Quality Criteria

### Reliability

모바일 client 오류가 Herdr 또는 agent process를 종료해서는 안 된다.

### Data integrity

Chat UI를 위해 agent transcript를 수정해서는 안 된다.

### Compatibility

agent CLI version 변경으로 structured parsing 일부가 실패하더라도 Terminal View는 계속 사용할 수 있어야 한다.

### Simplicity

새로운 persistence나 abstraction은 명확한 필요가 있을 때만 추가한다.

---

# 35. Major Risks

## Transcript format changes

Claude Code 또는 Codex transcript는 외부 consumer를 위한 stable API가 아닐 수 있다.

따라서 adapter 경계를 명확히 하고 parser failure가 전체 시스템 failure로 이어지지 않도록 한다.

---

## Interactive terminal state

모든 CLI interaction을 transcript에서 재구성할 수 없다.

Terminal fallback을 first-class capability로 유지한다.

---

## PWA background limitations

모바일 OS가 background connection이나 notification을 제한할 수 있다.

실제 사용 결과 이것이 핵심 UX를 심각하게 제한한다면 Web UI를 유지하면서 별도 native client를 검토한다.

---

## Scope expansion

파일 편집, Git, preview, browser 등의 기능을 추가하기 시작하면 모바일 IDE가 될 가능성이 높다.

MVP의 핵심 질문은 항상 다음이어야 한다.

> 이 기능이 agent와 모바일에서 자연스럽게 대화하기 위해 필요한가?

아니라면 기본적으로 범위 밖이다.

---

# 36. Architecture Decision Summary

현재까지 확정된 주요 결정은 다음과 같다.

| Decision                    | Choice                           |
| --------------------------- | -------------------------------- |
| Runtime owner               | Herdr                            |
| Agent owner                 | Claude Code / Codex              |
| Conversation source         | Native transcript                |
| Interactive source of truth | PTY                              |
| Chat model                  | Projection over existing session |
| User input path             | Existing Herdr pane              |
| First client                | Web / PWA                        |
| Network                     | Tailscale-only                   |
| User model                  | Single user                      |
| Host model                  | Single host                      |
| Primary UX                  | Chat-first                       |
| Terminal                    | Fallback                         |
| Conversation DB             | No independent source of truth   |
| Initial agents              | Claude Code, 이후 Codex           |

---

# 37. Core Product Definition

한 문장으로 정의하면:

> **Herdr에서 이미 실행 중인 coding agent를 모바일에서 메신저처럼 확인하고 이어서 대화할 수 있게 하는 private chat-first client.**

조금 더 기술적으로 정의하면:

> **A mobile-first projection layer over existing Herdr-managed agent sessions, combining Herdr lifecycle state, native agent transcripts, and PTY control into a unified chat experience.**

---

# 38. MVP Product Boundary

최종적으로 MVP는 다음 영역까지만 책임진다.

```text
          Herdr
            │
      Existing Agent
            │
     Native Transcript
            │
            ▼
      Herdr Bridge
       │         │
       │         └──── PTY
       │
       ▼
 Unified Conversation
       │
       ▼
    Mobile PWA
       │
       ├ Chat
       ├ Attention
       ├ Changes
       └ Terminal fallback
```

이 경계를 넘는 기능은 실제 사용 과정에서 명확한 필요가 확인되기 전까지 추가하지 않는다.
