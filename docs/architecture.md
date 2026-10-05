# Herdr Mobile Chat — Architecture

**Related:** [PRD](prd.md), [ADR](adr.md), [Issues](https://github.com/psw7205/herdr-remote/issues), [Herdr patch runbook](herdr-patch.md)

## 1. 개요

ADR의 결정을 조합해 현재 구현된 runtime 구조, data flow, invariant를 정리한다. Bridge는
Herdr에서 이미 실행 중인 Claude Code session을 발견하고, native transcript를 읽어 Chat으로
projection하며, 입력은 Herdr가 현재 `runtime_binding`을 검증하는 `agent.bound_input`으로만
전달한다. Codex session은 Herdr가 보고한 native session의 rollout을 read-only Chat으로만 보인다.
Bridge는 agent를 직접 실행하거나 resume하지 않고 conversation을 저장하지 않는다. 새 session은
사용자 요청이 있을 때 Herdr 공개 API로만 요청한다. session 폴더의 Git 변경은 `git` CLI로 읽기만 한다. 결정의 근거는
각 ADR을, 검증 범위는 [검증 기록](records/verification.md)을, 남은 작업은 [GitHub Issues](https://github.com/psw7205/herdr-remote/issues)를 따른다.

### 용어

| 용어 | 뜻 |
| --- | --- |
| Bridge | `cmd/bridge`의 Go process. Herdr socket과 native transcript를 읽어 browser에 HTTP/WS로 제공한다 |
| native transcript | agent가 직접 쓰는 대화 기록 파일. Claude Code에서는 session별 JSONL, Codex CLI에서는 session별 rollout JSONL이다. Bridge는 읽기만 한다 |
| `runtime_binding` | Herdr `agent.binding`이 발급한 token. pane의 terminal, foreground process, native session을 묶는다. 입력은 이 값이 아직 유효할 때만 전달된다 |
| 조건부 입력 (conditional input) | `agent.bound_input`처럼 Herdr가 binding을 다시 검증한 뒤에만 PTY에 쓰는 입력. stock Herdr에는 없고 patch가 추가한다 |
| incarnation | 같은 pane에서 실행된 특정 process의 한 번의 생애. PID만으로는 구분하지 않고 시작 시각과 native session으로 확인한다 |
| fail closed | 검증할 수 없으면 허용하지 않는 동작. binding이나 capability가 없으면 입력과 Terminal을 막는다 |
| Refresh | Bridge가 2초마다 Herdr에서 agent 목록과 binding을 다시 읽어 session item을 갱신하는 주기 |
| item | 목록의 session 단위. 검증된 Claude native session은 `claude:<native-id>`, Herdr가 보고한 Codex native session은 `codex:<native-id>`, 식별 전 pane은 `pane:<pane-id>`다 |
| `lifecycle` / `status` | `lifecycle`은 item의 검증·종료 상태(`active`, `unverified`, `ended` 등), `status`는 agent의 작업 상태(`working`, `idle` 등)다 |
| cursor (epoch, sequence) | client가 받은 마지막 event 위치. epoch는 Bridge restart나 transcript 재동기화 때 바뀐다 |
| snapshot / replay | 재연결 시 buffer에 남은 event를 이어 보내면 replay, 그렇지 못하면 전체 대화를 다시 보내면 snapshot이다 |
| command receipt | `command_id`별 전달 결과를 disk에 남긴 기록. 같은 ID의 재전송을 막는다. 대화 본문은 저장하지 않는다 |
| `delivery_unknown` | PTY 전달 여부를 확정할 수 없는 결과. 자동으로 다시 보내지 않는다 |

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
│                        (internal/claude, internal/codex)                 │
│                                   │                                      │
│                              herdr.Gateway                               │
└───────────────────────────────────┬──────────────────────┬───────────────┘
                   Herdr JSON socket │                      │ read-only file
┌───────────────────────────────────▼──────────────────────▼───────────────┐
│ Herdr host                                                               │
│  Herdr server (mobile-binding patch: agent.binding, agent.bound_input)   │
│   └ Workspace └ Tab └ Pane └ PTY └ Claude Code·Codex ──→ native JSONL    │
└──────────────────────────────────────────────────────────────────────────┘
```

`cmd/doctor`는 같은 Herdr socket과 Tailscale CLI를 read-only로 조회하는 별도 진단 binary다.

## 3. Code 구성

| 경로 | 책임 |
| --- | --- |
| `cmd/bridge/` | flag 해석(`-claude-dir`, `-codex-dir` 포함), loopback bind 검사, receipt store·registry·HTTP server 조립, 2s Herdr refresh loop |
| `cmd/doctor/` | Herdr capability(`conditional_input`, `verified_binding`)와 `tailnet` 설정의 read-only 진단 |
| `internal/herdr/` | Herdr JSON socket client. `session.snapshot`, `agent.binding`, `agent.bound_input`, `pane.read`, `pane.layout`, `pane.process_info`(doctor 전용), 새 session용 `workspace.create`·`tab.create`·`agent.start`·`agent.get`. 미지원 method는 `ErrUnsupported` |
| `internal/session/` | agent discovery, binding 검증, item identity·lifecycle, agent별 transcript adapter 선택, transcript watcher 수명, bound input·Terminal 전 binding 재확인, 새 session 후보와 시작(`start.go`) |
| `internal/claude/` | native transcript 경로 resolve, JSONL record decode, parent chain projection |
| `internal/codex/` | Codex rollout 경로 resolve와 `session_meta` 확인, record decode, 선형 projection |
| `internal/transcript/` | agent 공통 `Record`, offset 기반 incremental read(`Tailer`), `fsnotify` + polling reconciliation(`Watch`) |
| `internal/command/` | `command_id`별 durable receipt. conversation 내용은 저장하지 않는다 |
| `internal/stream/` | session별 snapshot·replay ring·live 구독의 원자적 경계, epoch/sequence |
| `internal/httpapi/` | HTTP routes, WebSocket, Host·Origin·Tailscale identity 경계, security header, build stamp 보고(`build.go`) |
| `internal/tailnet/` | `tailscale status --json`, `tailscale serve status --json` 조회와 host·owner login 자동 감지 |
| `internal/gitstate/` | session project의 Git 변경 목록·줄 수·파일 diff를 `git` CLI로 읽기. repo config의 명령 실행과 index 쓰기를 막는다 |
| `web/src/` | thin client: session 목록, Chat(Markdown), Composer, xterm.js Terminal, 변경된 파일·diff, command 재시도 상태 |
| `web/public/` | PWA manifest, icon, service worker |
| `web/plugins/` | Vite build plugin. bundle hash를 `index.html`의 `herdr-build` meta와 service worker cache 이름에 stamp |

## 4. Data flow

### 4.1 대화 읽기

```text
Bridge 2s tick → Registry.Refresh
  → Herdr session.snapshot (agent 목록)
  → agent == claude인 pane마다 agent.binding
      검증 조건: agent claude, terminal_id 일치, token·process_id·native_session_id 존재
  → claude.Resolve: <claude-dir>/projects/*/<native-id>.jsonl 이 정확히 하나일 때만 Chat 허용
  → item.startWatch → transcript.Watch
      transcript 파일 하나만 fsnotify 등록 후 초기 read, 이후 event 또는 1s tick마다 offset 이후만 read
      삭제·rename·등록 실패면 polling을 유지하며 tick마다 다시 등록
      file identity 변경·truncate·head 불일치면 Batch.Reset
  → claude.Decode + Projection (최신 leaf의 parent chain 중 text record와 tool 호출 item)
  → stream.Session
      reset/첫 load 또는 앞부분 text·순서 변경 → Reset: 새 epoch + `session.snapshot` event
      그 밖 → Commit: 바뀐 tool item은 `message.updated`, 추가된 item은 `message.user` /
              `message.assistant` / `message.tool` event, sequence++
  → WS 구독자
```

- 같은 native ID가 한 Refresh에서 두 pane에서 검증되면 어느 쪽도 binding과 함께
  `claude:<native-id>`를 차지하지 않는다([ADR-034](adr.md#adr-034--herdr가-runtime-binding을-검증한-command만-전달한다)).
- Codex pane은 `agent.binding`을 호출하지 않아 조건부 입력 판정에 섞이지 않는다. Herdr `agent_session`
  (agent `codex`, kind `id`)이 보고한 ID로 `codex.Resolve`가
  `<codex-dir>/sessions/*/*/*/rollout-*-<native-id>.jsonl` 하나와 첫 줄 `session_meta.payload.id`를 확인하면
  `codex:<native-id>` item을 만들고 같은 watcher·stream 경로로 읽는다. `codex.Decode`는 `response_item`
  message 중 kind가 `user.text`인 user 입력과 assistant `output_text`만 남기고 `event_msg`는 무시한다.
  message ID는 record의 `ordinal`이다([ADR-038](adr.md#adr-038--codex-chat은-herdr가-보고한-native-session으로-read-only-표시한다)).
- Refresh마다 `active`·`unverified` item의 transcript를 native ID로 다시 찾는다. 경로가 바뀌면
  watcher를 새 경로로 다시 시작해 새 epoch snapshot을 보낸다. 두 곳에서 보이거나 찾지 못하면 기존 경로를 유지한다.
- Claude `tool_use`는 같은 chain의 `tool_result`와 짝지은 role `tool` item이 된다. result가 오면 같은 item을
  `message.updated`로 갱신한다. 형식이 다른 tool block은 버리거나 빈 값으로 두고 transcript를 invalid로 만들지
  않는다. 상태 규칙과 상한은 [ADR-039](adr.md#adr-039--tool-호출은-chat-item으로-투영하고-결과는-message-갱신으로-보낸다)를 따른다.
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
      rejected + code   → 409 (RUNTIME_BINDING_MISMATCH, SESSION_ENDED, AGENT_NOT_READY, HERDR_UNSUPPORTED, SESSION_CHANGED …)
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

- `cols`·`rows`는 pane rect라 border·scrollbar cell을 포함한 상한이다. xterm의 local grid는 이 값과
  frame의 줄 수·가장 긴 visible line 폭 중 큰 값이고, Bridge 값이 없으면 최근 5개 poll의 최댓값이다
  (`web/src/terminalSizing.ts`). 폭은 xterm 기본 Unicode V6 폭표로 센다.
- font size는 화면 폭에 맞춰 6–16px에서 고르고, 6px로도 넘치면 가로 scroll한다. xterm이 pinch를
  삼킬 수 있어 `A−`/`맞춤`/`A+` 버튼으로 맞춘 크기에 배율을 곱한다(최대 32px).
- `pane.read`와 `pane.layout`은 별도 호출이라 그 사이의 resize를 구분하지 못한다.

### 4.5 새 session 시작

```text
GET /api/start-candidates
  → -project-root 아래 Git repo(root 자체 또는 한 단계 아래, hidden·root 밖 symlink 제외)
    + session.snapshot의 열린 workspace(첫 pane cwd) → opaque 후보 ID
POST /api/sessions  { command_id, command_type: session_start,
                      payload{candidate_id, kind: claude, placement: new_workspace | new_tab} }
  → command.Store.Execute (digest: 후보·kind·배치, runtime_binding 없음)
  → dispatch 직전 경로 재해석(EvalSymlinks, root 안·workspace 열림 확인). 실패 → 409 CANDIDATE_CHANGED
  → workspace.create 또는 tab.create (focus: false, env 없음) → root pane
  → agent.start(인자 없음) → agent.get polling으로 준비 대기
  → receipt에 생성된 workspace·tab·pane ID 기록. start 한 건은 50s 제한(client 60s)
```

- 새 session은 특별 경로 없이 4.1 discovery를 따른다. `pane:<pane-id>`로 보이고 native session이
  검증되면 `claude:<native-id>`가 되며, 그 뒤에만 Chat 입력이 열린다.
- 신뢰 확인 같은 시작 화면에서 멈추면 `AGENT_NOT_READY`로 끝나고, binding이 없어 PC의 Herdr에서 수락한다.
- 근거: [ADR-037](adr.md#adr-037--새-session-생성은-herdr에-요청한다), [ADR-033](adr.md#adr-033--command-receipt로-retry-중복-실행을-방지한다).

### 4.6 변경된 파일

```text
Chat의 변경된 파일 버튼 → #session=<id>&view=changes
  → GET /api/sessions/{id}/changes
  → Registry.Project(id): Herdr가 보고한 item의 project (client는 경로를 보내지 않음)
  → gitstate.Reader (git 실행 동시 2개, 각 10s, 고정 인자, shell 없음)
      git rev-parse --show-toplevel        실패 "not a git repository" → repository: false
      git config --get-regexp ^filter\.    정의된 filter driver 이름
      git status --porcelain=v2 -z --branch --untracked-files=all
      git diff-index --numstat -z <HEAD 또는 empty tree>
      untracked regular file은 os.Root 안에서 직접 읽어 줄 수
  → { repository, branch, initial, files[{path, old_path, status, untracked, binary, additions, deletions}], total, truncated }
파일 선택 → #...&view=changes&file=<path>
  → GET /api/sessions/{id}/changes/diff?path=<path>
  → status를 다시 읽어 path가 목록에 정확히 있을 때만
      tracked: git diff-index -p <base> -- <path> [<old_path>]
      untracked: 직접 읽은 내용을 unified diff로 구성
  → { ...file, content: text | binary | none, diff, truncated }
```

- 비교 기준은 HEAD(index와 working tree를 합친 상태)이고, commit이 없으면 empty tree다. status의 `R`은
  이름 변경, `?`는 추가(untracked), `D`는 삭제, 나머지는 수정이다. 같은 worktree의 사람 변경도 섞이므로
  UI는 "변경된 파일"로만 부르고 agent에 귀속하지 않는다.
- 모든 git 실행은 고정 환경(`PATH`, `HOME`, `XDG_CONFIG_HOME`, `LC_ALL=C`, `GIT_OPTIONAL_LOCKS=0`,
  `GIT_LITERAL_PATHSPECS=1` 등)과 `--no-optional-locks`를 쓰고, `GIT_CONFIG_COUNT`로 `core.fsmonitor=false`,
  `core.hooksPath`, 모든 filter driver의 clean·smudge·process 비우기를 덮는다. diff는 `--no-ext-diff`,
  `--no-textconv`, `--no-color`, `--ignore-submodules=dirty`다. porcelain `git diff <commit>`은 index를 다시
  쓰므로 plumbing `diff-index`를 쓴다.
- 상한: 목록 500개, status·numstat 출력 8 MiB, 파일 diff 256 KiB, untracked 줄 수는 파일당 4 MiB·요청당
  32 MiB까지 센다. 넘으면 `truncated`이거나 줄 수가 없다.
- server polling, watcher, WS event, 저장은 없다. client가 화면을 열거나 새로 고칠 때만 읽는다.
- 근거: [ADR-016](adr.md#adr-016--changed-files는-git을-read-only-source로-사용한다),
  [ADR-014](adr.md#adr-014--file-editing은-제공하지-않고-review까지만-지원한다).

## 5. HTTP/WS API

| Method·Path | 형태 | 역할 |
| --- | --- | --- |
| `GET /api/sessions` | JSON | `ended`·`superseded`가 아닌 item의 `Meta` 목록, `herdr.conditional_input`(`supported`/`unsupported`/`unknown`), 허용 kind와 새 workspace 가능 여부를 담은 `start`, 실행 중인 code를 알리는 `bridge`(`go build`가 심은 `revision`·`modified`, `web/dist/index.html`에 stamp된 `client_build`). web은 3s poll |
| `POST /api/sessions` | JSON | `session_start`. Origin 검사. 결과와 생성된 workspace·tab·pane ID |
| `GET /api/start-candidates` | JSON | 새 session 후보 폴더와 열린 workspace의 opaque ID·표시 이름 |
| `GET /api/sessions/{id}` | JSON | `session`(`Meta`)과 `snapshot`(`cursor`, `data`). Chat 화면은 metadata를 3s poll |
| `GET /api/sessions/{id}/events` | WebSocket | query `epoch`, `sequence`. replay 또는 `session.snapshot` 뒤 live event. Origin 검사 |
| `GET /api/sessions/{id}/terminal` | JSON, 1s poll | `X-Runtime-Binding` header 필수. visible ANSI frame과 `cols`/`rows` |
| `POST /api/sessions/{id}/commands` | JSON | `prompt` / `interrupt` / `terminal_input`. Origin 검사. 결과 `accepted`/`rejected`/`delivery_unknown` |
| `GET /api/sessions/{id}/changes` | JSON | project의 변경된 파일 목록(4.6). Origin이 있으면 allowlist 검사. 오류 `SESSION_NOT_FOUND`, `PROJECT_NOT_FOUND`, `GIT_UNAVAILABLE`, `GIT_TIMEOUT`, `GIT_FAILED` |
| `GET /api/sessions/{id}/changes/diff` | JSON | query `path` 하나(목록의 path). 파일 unified diff. 그 밖의 path는 `FILE_NOT_CHANGED`, 형식 오류는 `INVALID_PATH`, repository가 아니면 `NOT_A_REPOSITORY` |
| `/` | static | `-static` directory(`web/dist`). index 없는 directory listing 금지 |

WS event envelope은 `protocol_version`(1), `event_id`(`<epoch>:<sequence>`), `session_id`, `cursor`,
`timestamp`, `type`, `payload`, `source`다. 현재 type은 다음 일곱 가지다.

| Type | 출처 | Payload |
| --- | --- | --- |
| `session.snapshot` | `bridge` | `cursor`, `data`(Message 배열). Herdr socket method `session.snapshot`과 이름만 같다 |
| `message.user`, `message.assistant` | `claude.transcript`, `codex.transcript` | `id`, `role`, `text`, `timestamp` |
| `message.tool` | `claude.transcript` | 위 field(`role` `tool`, `text` 빈 값)와 `tool`: `id`, `name`, `summary`, `state`(`running`·`completed`·`error`·`unknown`), `input`, `input_truncated`, `result`, `result_truncated` |
| `message.updated` | `claude.transcript` | 이미 보낸 tool item 전체. client는 같은 `id`를 교체한다 |
| `agent.status` | `bridge` | `status`, `lifecycle`, 필요 시 `successor_id` |
| `session.error` | `bridge` | `message` (Terminal 전환 안내) |

`Meta`는 `id`, `agent`, `pane_id`, `project`, `title`, `status`, `runtime_binding`, `chat`, `terminal`,
`active`, `lifecycle`, `successor_id`, `last_activity`, `last_message`를 가진다. client는 native
transcript 형식을 모른다([ADR-007](adr.md#adr-007--unified-event-protocol을-client-contract로-사용한다),
[ADR-027](adr.md#adr-027--resthttp--websocket-hybrid-transport를-사용한다)).

## 6. Session lifecycle 요약

- item ID는 검증된 Claude native session이면 `claude:<native-id>`, Herdr가 보고한 Codex native session이면
  `codex:<native-id>`, 식별 전이면 `pane:<pane-id>`다.
- `lifecycle`: `active`, `unverified`, `unbound`, `superseded`(+`successor_id`), `ended`. `unbound`는 검증된
  binding을 가진 적이 없는 item(`pane:` item, 모든 `codex:` item)이며 모든 write가 fail closed한다.
- `codex:` item은 binding과 Terminal 없이 Chat만 read-only로 보인다. pane이 사라지거나, Herdr가 그 pane에서
  codex를 더 이상 보고하지 않거나, `agent_session`이 다른 ID를 가리키면 `ended`다. 같은 pane의 `pane:`
  item은 새로 생긴 `codex:` item으로 supersede된다.
- `status`: Herdr `idle`/`done`/`working`/`blocked`를 `idle`/`completed`/`working`/`needs_attention`으로,
  그 밖은 `error`로 옮긴다.
- binding이 없는 상태에서는 prompt, interrupt, Terminal read/input이 모두 fail closed한다.
- transcript watcher는 `active`·`unverified` 동안만 돈다. `ended`·`superseded`가 되면 닫고, 다시
  `active`가 되면 처음부터 읽어 새 epoch으로 동기화한다.
- 종료된 item은 stream snapshot만 남기고 parsed history를 해제하며, 최근 종료된 32개만 유지한다.
- `GET /api/sessions`는 `ended`·`superseded` item을 빼지만 `GET /api/sessions/{id}`와 WS는 계속 제공한다.
  client는 3s 목록 poll에서 열린 item이 빠지면 `GET /api/sessions/{id}`의 `successor_id`를 읽고, 목록에 있는
  successor와 그 binding으로 Chat·Terminal을 전환한다.

continuity, supersession, `ended` 판정 규칙은
[ADR-034](adr.md#adr-034--herdr가-runtime-binding을-검증한-command만-전달한다)가, Codex는
[ADR-038](adr.md#adr-038--codex-chat은-herdr가-보고한-native-session으로-read-only-표시한다)이 기준이며 여기서 반복하지 않는다.

## 7. 보안 경계

- `cmd/bridge`는 `-listen`이 `localhost`/`127.0.0.1`/`::1`이 아니면 종료한다(exit 2).
- `httpapi.Handler`는 모든 요청에 `nosniff`, `X-Frame-Options: DENY`, `frame-ancestors 'none'`을 붙이고
  `Host`를 loopback host 또는 확정된 tailnet host(`:443` 포함)로 제한한다.
- tailnet host, tailnet Origin, 또는 `X-Forwarded-*`·`Tailscale-User-*` header 중 하나라도 있으면
  tailnet 요청으로 본다. 이때 tailnet host·owner login이 확정돼 있고 `Tailscale-User-Login`이
  owner와 같으며 `Host`가 tailnet host여야 한다. 그래서 Serve 외 local reverse proxy는 지원하지 않는다.
- `GET .../changes`, `GET .../changes/diff`는 source를 돌려주므로 `Origin` header가 있으면 allowlist를
  검사한다. 같은 origin의 GET은 browser가 Origin을 보내지 않으므로 header 없는 요청은 Host·identity 경계만 거친다.
  Git 경로는 registry의 item project에서만 정하고 diff `path`는 현재 status 목록과 정확히 같아야 한다.
- `POST .../commands`와 WS는 exact Origin allowlist를 검사한다. `-origins` 기본값에 더해 host와
  login이 모두 확정되면 `https://<tailnet-host>`를 추가한다. login 없는 tailnet Origin은 제외한다.
- Tailnet host·login은 `-tailnet-host`·`-tailnet-login`(기본 `auto`)으로 시작 시 한 번 해석하고,
  실패하면 localhost 전용으로 동작한다. Funnel과 wildcard CORS는 지원하지 않는다.

근거와 위협 모델은 [ADR-024](adr.md#adr-024--tailnet을-primary-security-boundary로-사용한다)를 따른다.
network 계층 ACL은 [P0-03](https://github.com/psw7205/herdr-remote/issues/2)이다.

## 8. Invariants

| ID | Invariant | 강제 위치 |
| --- | --- | --- |
| I1 | Chat이 가능한 item 하나는 Herdr가 검증하거나(Claude binding) 보고한(Codex `agent_session`) 기존 native session 하나다 | `internal/session` `claude:`·`codex:<native-id>` identity, `internal/claude`·`internal/codex` `Resolve`의 단일 경로 조건 |
| I2 | Mobile 입력은 agent process를 만들지 않는다. 새 session은 별도 명시 command로만 Herdr에 요청한다(ADR-037) | 입력은 `agent.bound_input`만. 생성은 `session_start`의 `workspace.create`·`tab.create`·`agent.start`만이며 resume·agent 인자 경로 없음 |
| I3 | Transcript는 read-only다 | `internal/transcript`는 `os.Open`만 사용. Bridge의 disk write는 `internal/command` receipt뿐 |
| I4 | browser/WS 종료는 agent, pane, session에 영향을 주지 않는다 | `internal/httpapi` WS 종료 시 구독 해제만 수행 |
| I5 | Agent-specific parsing은 Bridge 안에만 있다 | `internal/claude`, `internal/codex` |
| I6 | Client는 native transcript format을 모른다 | `internal/session` `Message{id, role, text, timestamp, tool}`와 `internal/stream` envelope |
| I7 | Chat이 표현하지 못하거나 transcript가 불명확하면 같은 pane의 Terminal로 fallback한다 | `session.error`, `chat: false`, `internal/session` `Registry.Terminal` |
| I8 | Bridge restart 뒤 Herdr와 native transcript에서 상태를 재구성한다 | `cmd/bridge` 시작 Refresh, 새 epoch snapshot, `-receipts-dir` receipt 재사용 |
| I9 | Bridge는 conversation의 유일한 copy가 아니다 | stream state는 memory projection. receipt는 digest와 결과만 저장 |
| I10 | Bridge 접근은 localhost 또는 Tailscale Serve 경유만 허용한다 | `cmd/bridge` loopback bind 검사, `internal/httpapi` Host·identity·Origin gate |
| I11 | 모든 write는 현재 `runtime_binding`을 Herdr가 검증한 경로로만 전달한다. binding이 없는 Codex item은 write가 없다 | `internal/session` `BoundInput` 사전 검사, Herdr `agent.bound_input` |
| I12 | 같은 `command_id`는 한 번만 dispatch하고 `delivery_unknown`을 자동 재전송하지 않는다 | `internal/command` `Store.Execute`, `web/src` `commandDelivery` |
| I13 | Terminal은 Herdr PTY 크기를 바꾸지 않는다 | `internal/herdr`는 read-only `pane.read`·`pane.layout`만 호출 |
| I14 | Changed Files는 Git repository를 바꾸지 않고 repo config의 명령을 실행하지 않는다 | `internal/gitstate`의 고정 인자·환경, `--no-optional-locks`·`diff-index`, filter·fsmonitor·hook·외부 diff 차단. `TestRepoConfigRunsNothing`이 marker와 index 불변으로 확인 |

실제 검증 범위와 미검증 scenario는 [검증 기록](records/verification.md)을 따른다.

## 9. 기술 선택

| 영역 | 결정 |
| --- | --- |
| Bridge | Go `net/http`(method pattern routing), `coder/websocket`, `fsnotify`, `log/slog`. 단일 process([ADR-010](adr.md#adr-010--bridge는-single-process-control-plane으로-시작한다)) |
| Client | React + TypeScript + Vite, pnpm. Terminal은 `@xterm/xterm` |
| Toolchain | `mise.toml` |
| Persistence | `command_id`당 JSON receipt 파일 하나(`O_EXCL` reserve, fsync, temp+rename finish). conversation DB·Redis·broker 없음 |
| Transport | semantic event는 WebSocket, Terminal frame은 HTTP polling으로 물리적으로 분리([ADR-028](adr.md#adr-028--terminal-stream과-semantic-event-stream을-논리적으로-분리한다)) |
| Transcript watch | transcript 파일 `fsnotify` + 1s polling reconciliation, offset 이후 incremental read |

receipt 보존 기간과 buffer 운영은 [P2-06](https://github.com/psw7205/herdr-remote/issues/19), 장기 실행
resource 상한은 [P1-13](https://github.com/psw7205/herdr-remote/issues/12)에서 다룬다.

## 10. 미구현 경계

| 영역 | 현재 | Issue |
| --- | --- | --- |
| Codex 입력 | Chat은 read-only다. Herdr Codex binding이 없어 prompt·interrupt·Terminal 없음. 실제 Herdr Codex pane의 hook 보고→Chat 표시는 미실측 | [P1-01](https://github.com/psw7205/herdr-remote/issues/4), [P1-02](https://github.com/psw7205/herdr-remote/issues/5) |
| Structured permission/question | Terminal fallback만 있다 | [P2-02](https://github.com/psw7205/herdr-remote/issues/15) |
| Attachment | route와 host file 처리 없음 | [P2-04](https://github.com/psw7205/herdr-remote/issues/17) |
| Notification | Web Push 없음 | [P2-05](https://github.com/psw7205/herdr-remote/issues/18) |
| Streaming delta | 완성된 JSONL text record만 전송하고, 그 사이는 `agent.status`의 `working`으로 표시한다. Claude transcript는 API message가 끝난 뒤 content block마다 완성된 record를 써서 delta source가 없다([실측](records/integration-findings.md#2026-10-05--claude-transcript-live-기록-단위와-tool-식별자)). Codex는 미측정 | [P1-07](https://github.com/psw7205/herdr-remote/issues/9) |
| 새 session의 다른 kind·worktree | `claude`만 허용. worktree 생성 없음 | [P1-01](https://github.com/psw7205/herdr-remote/issues/4), [P1-02](https://github.com/psw7205/herdr-remote/issues/5), [P2-09](https://github.com/psw7205/herdr-remote/issues/22) |

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
