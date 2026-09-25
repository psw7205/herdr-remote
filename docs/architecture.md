# Herdr Mobile Chat — Architecture Overview

**Related:** `docs/prd.md`, `docs/adr.md`

ADR의 개별 결정을 조합한 runtime 구조, data flow, API boundary, invariant, fitness test와
기술 선택을 정리한다. 각 결정의 근거와 상태는 `docs/adr.md`를 따른다.

---

# Proposed Runtime Architecture

전체 구조는 다음과 같다.

```text
┌───────────────────────────────────────────────┐
│                 Mobile PWA                    │
│                                               │
│  Sessions   Chat   Changes   Terminal         │
└───────────────────┬───────────────────────────┘
                    │
             HTTP / WebSocket
                    │
             Tailscale Serve
                    │
┌───────────────────▼───────────────────────────┐
│                  Bridge                       │
│                                               │
│  ┌───────────────────────┐                    │
│  │ Session Registry      │                    │
│  └───────────┬───────────┘                    │
│              │                                │
│  ┌───────────▼───────────┐                    │
│  │ Agent Adapter Registry│                    │
│  │                       │                    │
│  │ Claude       Codex    │                    │
│  └───────────┬───────────┘                    │
│              │                                │
│  ┌───────────▼───────────┐                    │
│  │ Transcript Watchers   │                    │
│  └───────────┬───────────┘                    │
│              │                                │
│  ┌───────────▼───────────┐                    │
│  │ Event Normalizer      │                    │
│  └───────────┬───────────┘                    │
│              │                                │
│  ┌───────────▼───────────┐                    │
│  │ Event Buffer          │                    │
│  └───────────────────────┘                    │
│                                               │
│  Herdr Gateway ────────────── Terminal Gateway│
│                                               │
│  Attachment / Diff / Notification             │
└───────────────┬───────────────────┬───────────┘
                │                   │
         Herdr Socket          Filesystem
                │                   │
┌───────────────▼───────────────────▼───────────┐
│                 Herdr Host                    │
│                                               │
│ Workspace                                     │
│   └ Tab                                       │
│      └ Pane                                   │
│         └ PTY                                 │
│            └ Claude / Codex                   │
│                    │                          │
│                    └ Native Transcript        │
└───────────────────────────────────────────────┘
```

---

# Data Flow — Reading Conversation

```text
Herdr
  │
  │ pane + native session id
  ▼
Session Registry
  │
  ▼
Agent Adapter
  │
  │ locate transcript
  ▼
Native Transcript
  │
  ▼
Incremental Parser
  │
  ▼
Normalized Event
  │
  ├── Event Buffer
  │
  ├── Notification
  │
  └── WebSocket
          │
          ▼
       Chat UI
```

---

# Data Flow — Sending Prompt

```text
Composer
    │
    ▼
POST /session/:id/prompt
    │
    ▼
Session Registry
    │
    ▼
resolve pane
    │
    ▼
Herdr Gateway
    │
    ▼
PTY Input
    │
    ▼
Existing Agent
    │
    ▼
Native Transcript
    │
    ▼
Normal read pipeline
```

중요한 점은 response를 HTTP request의 response body로 반환하지 않는 것이다.

```text
POST prompt
    ↓
Accepted

Agent response
    ↓
Transcript
    ↓
Event stream
```

즉 command path와 observation path를 분리한다.

---

# Data Flow — Reconnection

```text
Client
 last sequence = 501
       │
       X
   disconnected


agent continues


Client
       │
       │ connect
       │ session=abc
       │ after=501
       ▼
Bridge
       │
       ├─ buffer has events?
       │
       ├ YES → replay 502...
       │
       └ NO  → snapshot
       │
       ▼
live subscription
```

---

# Suggested API Boundary

정확한 endpoint naming은 implementation 단계에서 조정할 수 있지만 역할은 다음 정도로 제한한다.

## HTTP

```text
GET  /sessions

GET  /sessions/:id

GET  /sessions/:id/messages

POST /sessions/:id/messages

POST /sessions/:id/interrupt

GET  /sessions/:id/changes

GET  /sessions/:id/diff

POST /sessions/:id/attachments
```

## Live

```text
WS /events
```

subscription concept:

```text
session
after_sequence
```

## Terminal

```text
WS /sessions/:id/terminal
```

Terminal과 semantic event channel을 별개로 유지한다.

---

# Initial Domain Model

```text
Session
├ id
├ paneRef
├ agent
├ nativeSessionId
├ project
├ status
├ capabilities
└ activity


ConversationEvent
├ id
├ sessionId
├ sequence
├ timestamp
├ type
└ payload


Agent
├ type
├ version?
└ capabilities


PaneRef
├ workspaceId
├ tabId
└ paneId
```

이 모델보다 복잡한 domain entity는 실제 필요가 생기기 전까지 추가하지 않는다.

---

# Architecture Invariants

구현 과정에서 다음 조건은 깨지면 안 된다.

### I1

```text
1 Chat Session
=
1 Existing Native Agent Session
```

### I2

Mobile prompt는 새로운 agent process를 생성하지 않는다.

### I3

Transcript는 read-only다.

### I4

Client disconnect가 agent process에 영향을 주지 않는다.

### I5

Agent-specific parsing은 Bridge 내부에만 존재한다.

### I6

Client는 native transcript format을 알지 않는다.

### I7

Chat에서 표현할 수 없는 interaction은 Terminal로 fallback할 수 있다.

### I8

Bridge restart 후에도 native transcript와 Herdr를 기반으로 상태를 재구성할 수 있다.

### I9

Mobile backend가 conversation의 유일한 copy가 되어서는 안 된다.

### I10

Tailnet 밖에서 Bridge에 직접 접근하는 구성을 기본으로 지원하지 않는다.

---

# Architecture Fitness Tests

구현 후 architecture가 의도대로 유지되고 있는지 다음 질문으로 검증한다.

## Runtime

* Mobile 연결 없이도 agent는 정상적으로 계속 동작하는가?
* Bridge를 종료해도 agent는 살아 있는가?
* Bridge를 다시 실행하면 기존 session을 다시 찾는가?

## Identity

* 같은 repository에서 Codex 두 개를 실행해도 올바른 transcript를 각각 찾는가?
* pane이 바뀌어도 native session identity를 유지할 수 있는가?

## Conversation

* Desktop 입력이 Mobile Chat에 나타나는가?
* Mobile 입력이 Desktop terminal에도 같은 session으로 나타나는가?
* duplicate messages가 발생하지 않는가?

## Reconnect

* 10분간 모바일 화면을 꺼도 작업이 계속되는가?
* foreground 후 누락된 응답을 복원하는가?

## Compatibility

* transcript parsing 일부가 깨져도 Terminal은 동작하는가?
* unknown event가 Client 전체를 깨뜨리지 않는가?

## Security

* Tailnet 외부에서 접근할 수 없는가?
* attachment가 지정 directory 밖을 읽거나 쓸 수 없는가?

---

# 기술 선택과 남은 결정

L1/L2/L4는 사용자 지정 stack으로 확정했다. L3 저장 형식과 L5 transport 세부사항은
구현 검증으로 결정하며 L6 Web Push는 후속 optional capability다.

## L1 — Backend language/framework

Go로 확정한다. `net/http`, `coder/websocket`, `fsnotify`, `log/slog`를 사용한다.

필요 조건:

```text
Herdr socket integration
filesystem watch
WebSocket
PTY proxy
low operational overhead
```

---

## L2 — Client framework

React + TypeScript + Vite로 확정한다. pnpm과 mise로 package/toolchain version을 관리한다.
Terminal UI는 xterm.js를 사용한다.

---

## L3 — Embedded persistence

conversation persistence는 추가하지 않는다. crash 뒤 command retry 중복을 막는 최소 durable receipt만
허용한다. 정확한 embedded 저장 형식은 fsync/atomic reserve/recovery 검증을 기준으로 선택한다.

---

## L4 — Transcript file watching strategy

후보:

```text
filesystem events
polling
hybrid
```

`fsnotify`를 주 경로로 하고 event 유실/rename 복구를 위한 제한적 reconciliation을 병행한다.
읽기는 offset 이후 incremental 방식이며 전체 transcript를 polling마다 parse하지 않는다.

---

## L5 — Semantic / Terminal WebSocket multiplexing

논리적으로는 분리하지만 물리 connection을 하나 또는 두 개 사용할지는 prototype 후 결정한다.

---

## L6 — Web Push

iOS / Android PWA 실제 동작을 검증한 후 notification implementation을 확정한다.

---

# Decision Priority

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

---

# Final Architecture Statement

Herdr Mobile Chat은 별도의 coding agent platform이 아니다.

다음 세 데이터를 결합하는 presentation/control layer다.

```text
Herdr
  → runtime state

Claude / Codex transcript
  → conversation state

PTY
  → interactive state
```

Bridge가 이 세 영역을 통합해 stable semantic protocol을 제공하고,

```text
Bridge
   ↓
Unified Session API
   ↓
Mobile PWA
```

Client는 이를 메신저 형태로 표현한다.

핵심 architecture 원칙은 다음 한 문장으로 요약한다.

> **Do not recreate the agent session; project the existing session into a mobile-friendly interface.**
