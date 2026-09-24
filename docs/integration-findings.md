# Herdr integration 조사

조사일: 2026-09-22. 기준: `docs/prd.md`, `docs/adr.md`, 제공된 implementation prompt.
이 문서는 구현 사실과 차단 조건을 기록한다. 제품 요구사항의 source of truth는 PRD/ADR이다.

## A. Repository findings

`herdr-remote`는 초기 commit `90d7327`에 PRD/ADR만 있으며 frontend/backend,
manifest, toolchain 설정, 테스트가 없다. 미추적 `.vscode/`는 기존 사용자 파일이다.

Herdr의 로컬 checkout은 `1491b7d`, package version `0.7.5`다. 설치된 client와
실행 중 server는 모두 `0.9.1`, private protocol `22`다. 버전 차이가 있으므로 아래
source-based 판단은 설치 버전의 [v0.9.1 source](https://github.com/herdrdev/herdr/tree/065ef9d6a531c49fb8bee7e818ef837065b21ee9)를 기준으로 한다.

| Capability | 판정 | 근거와 한계 |
| --- | --- | --- |
| 실행 중 pane/agent 목록 | Supported | 실제 `session.snapshot`, `agent.get`에서 workspace/tab/pane/terminal ID와 Claude를 관찰했다. |
| 현재 foreground process | Supported | 실제 `pane.process_info`에서 shell PID, foreground process group, Claude PID/argv를 얻었다. PID만으로 process incarnation을 식별하지 않는다. |
| Herdr native session association | Partially supported | schema의 `AgentInfo.agent_session`은 존재하지만 현재 agent에는 없다. `integration status`에서 Claude/Codex hook이 모두 미설치였다. |
| Claude transcript resolution | Partially supported | foreground PID에 대응하는 Claude `sessions/<pid>.json`의 `sessionId`, `procStart`를 읽고 실제 OS 시작 시간 및 transcript의 `sessionId`와 일치시켰다. Herdr association을 대신하는 write authority는 아니다. |
| Codex transcript resolution | Partially supported | native rollout의 `session_meta.payload.id` 및 실제 record 구조를 확인했다. 현재 Herdr에 Codex가 없어 pane→native session 연결은 검증하지 못했다. |
| 기존 PTY prompt | Partially supported | `agent.prompt` 및 ordered submission이 존재한다. expected native session/process generation을 받지 않아 stale write invariant는 충족하지 못한다. 실제 prompt는 보내지 않았다. |
| PTY output 관찰 | Partially supported | `agent.read`는 terminal snapshot이다. byte stream mirror와 동일하지 않다. direct attach는 private protocol이며 observer가 아닌 owner다. |
| 여러 observer | Partially supported | JSON snapshot 및 event subscription은 연결별로 가능하다. direct terminal attach는 terminal별 한 owner만 허용한다. |
| interrupt | Partially supported | `agent.send_keys`로 `ctrl+c`를 전달하는 경로는 있다. native session 조건부 입력이 없으므로 안전한 remote interrupt로 사용할 수 없다. 실제 interrupt는 보내지 않았다. |
| pane/process/session lifecycle | Partially supported | `pane.updated`, `pane.exited`, `pane.closed`, `pane.agent_detected`, `pane.agent_status_changed` subscription이 있다. shell PTY 종료와 그 안의 agent 종료는 다르며 agent incarnation 전용 generation은 없다. |
| disconnect isolation | Partially supported | 실제 JSON event subscription을 열고 닫은 뒤 같은 agent/terminal/status가 유지됐다. Bridge WebSocket과 실제 handoff acceptance는 아직 구현·검증 전이다. |

### 근거가 있는 Herdr 코드

모든 링크는 설치 버전 source로 고정했다.

- [`AgentInfo`, `AgentSessionInfo`, `AgentPromptParams`, `AgentSendKeysParams`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/api/schema/agents.rs): session association은 optional이다. prompt에는 `target`, `text`, `wait`만 있고 key input에는 `target`, `keys`만 있다. request `id`는 correlation field이며 command deduplication 보장이 아니다.
- [`queue_agent_prompt`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/app/api/agents.rs#L112): blocked/startup/foreground agent kind를 검사한 뒤 기존 PTY에 text/Enter를 enqueue한다. 화면을 연 시점의 native session이나 PID와 비교하지 않는다.
- [`runtime_hosts_agent`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/app/agents.rs#L426): 현재 foreground job의 agent **종류**를 비교한다. Claude A→Claude B를 구별하는 조건이 아니다.
- [`PtyIoDataCommand::SubmitUserInput`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/pty/actor/unix.rs#L76): text/Enter 사이 delay가 있으며 queued submission에는 expected binding이 없다. API handler에서만 검사해도 queue 안에서 바뀐 process를 보호하지 못한다.
- [`attach_terminal_client`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/server/headless.rs#L1798): owner 단독 점유, takeover, resize lock, PTY resize, `start_pending_agent_resume_for_terminal`이 포함된다. mobile mirror에 그대로 재사용하지 않는다.
- [`remove_client`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/server/headless.rs#L1014): attach owner/resize lock을 해제한다. observer detach를 agent 종료로 연결할 이유가 없다.
- [`Subscription`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/api/schema/events.rs): lifecycle events를 제공하지만 transcript event replay cursor는 없다. Bridge cursor와 구분해야 한다.
- [`Claude SessionStart hook`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/integration/assets/claude/herdr-agent-state.sh), [`Codex SessionStart hook`](https://github.com/herdrdev/herdr/blob/065ef9d6a531c49fb8bee7e818ef837065b21ee9/src/integration/assets/codex/herdr-agent-state.sh): native session을 `pane.report_agent_session`으로 전달한다. 설치만으로 이미 지나간 SessionStart를 소급 관찰할 수 있다고 가정하지 않는다.

### 실제 transcript 관찰

사용자 prompt/output 원문, session ID, host path는 fixture나 문서에 복사하지 않았다.

**Claude Code 2.1.278**

- 실제 interactive process의 PID metadata와 transcript를 연결했다. `procStart`는 UTC로 조회한 OS process 시작 시간과 일치했다. metadata의 `status`를 Herdr의 현재 interactive status로 대체하지 않는다.
- transcript는 약 16 MB의 JSONL이었고 조사 시 마지막 newline까지 완성되어 있었다. 모든 `sessionId`가 PID metadata와 일치했다. `cwd`/mtime로 최신 파일을 선택한 것이 아니다.
- `user` record에는 `uuid`, `parentUuid`, `sessionId`, `timestamp`, `message.role`, `message.content`가 있다. content는 string 또는 block array다. `tool_result` 역시 `user` record에 있으므로 전체 user record를 사용자 prompt로 표시하면 잘못된 projection이다.
- `assistant` record에는 text, tool use 등 content block이 있다. 최초 parser는 실제 `text`만 읽고 unknown block은 무시한다. `isSidechain`과 parent 관계를 보존해 다른 branch/subagent를 혼합하지 않는다.
- `system`, `attachment`, `queue-operation`, `file-history-*`, `last-prompt` 등의 record도 있다. generic permission/question card로 임의 변환하지 않는다.
- 안정적인 streaming delta와 permission/question response contract는 이 조사로 확정하지 않았다. 관찰된 완전한 message만 projection하고 Terminal fallback을 사용한다.
- 한 번의 완성된 파일 관찰로 append-only, truncate/rotation 부재, partial-write 형태까지 증명할 수 없다. watcher는 모두 방어하고 synthetic fixture로 검증해야 한다.

**Codex 0.155.0-alpha.9.2 / Desktop source**

- 실제 JSONL에서 `session_meta`, `response_item`, `event_msg`, `turn_context`, `world_state`, `token_usage_record`를 관찰했다.
- `session_meta.payload.id`/`session_id`, `cli_version`, `originator`, `source`가 존재한다. 이 sample은 Desktop에서 생성되었으므로 Herdr CLI 실행 schema 전체와 동일하다고 주장하지 않는다.
- `response_item.payload`의 `role`/`content`와 `event_msg`는 서로 다른 record이며 양쪽을 무작정 message로 normalize하면 중복 가능성이 있다.
- unresolved: 실행 중 Herdr Codex가 없어 native identity, PTY round-trip 및 CLI transcript compatibility를 검증하지 못했다.

첫 adapter 선택: **Claude**. 의도는 실제 기존 process에서 가장 짧게 handoff를 검증하는 것이다.
추천: Claude 하나부터 구현 — 이유는 foreground PID, native session metadata, transcript를 모두 실제로 연결했고 현재 Herdr에도 실행 중이기 때문이다.

## B. PRD/ADR patch plan

아래 결정은 이번 문서 patch에 반영한다. 구현 capability와 혼동하지 않는다.

| 문제 | 현재 위치 | 결정 / 수정 |
| --- | --- | --- |
| MVP가 Proposed 기능을 필수로 포함 | PRD §16/20/25/30, ADR-015/016/017/032 | attachment/changes/notification은 optional 후속 단계. Accepted ADR의 필수 조건에서 제외. |
| replay의 buffer 결정 미확정 | ADR-008/019 | bounded in-memory buffer를 Accepted로 확정. snapshot cursor 이후 replay와 live 등록을 같은 lock에서 처리. overflow는 snapshot 재동기화. |
| command retry 의미 없음 | PRD §13, 신규 ADR-033 | ID/target/binding/type/payload digest 단위 deduplication. pending receipt를 PTY 전송 전에 durable하게 기록. crash/timeout은 unknown이며 자동 재전송 금지. |
| stale pane write | PRD §8/13, ADR-003/034 | pane/terminal은 process incarnation이 아니다. Herdr가 발급하는 binding을 command 처리와 PTY dequeue 시 검증. 미지원이면 write disable. |
| sequence restart 충돌 | PRD §23, ADR-008/019 | cursor는 epoch+sequence. restart/replacement/resync 시 epoch 교체, 이전 epoch는 fresh snapshot. |
| browser security | PRD §21/22, ADR-024 | localhost bind, exact allowed Origin, mutation/WS 검증, wildcard CORS 금지, Tailnet ACL. |
| 상태/수명 혼용 | PRD §4/8/17 | status enum 통일, lifecycle 별도. completed는 turn 완료이며 process 종료가 아니다. |
| Terminal resize와 resume | PRD §19, ADR-013/035 | desktop가 size owner. mirror attach는 resize/resume/start하지 않고 detach는 observer만 제거. |
| 기술 스택 미정 | ADR L1/L2/L4 | 사용자 지정 Go/React/TypeScript/Vite/pnpm/mise stack 확정. fsnotify + reconciliation. |

## C. First vertical slice plan

파일별 실행 순서와 acceptance matrix는 [구현 계획](implementation-plan.md)에 있다.
핵심 순서는 Herdr capability gate → Claude projection → command path → mobile Chat → real handoff다.

## D. Risks / blockers

### B1 — session 조건부 입력 없음

`get → validate → prompt`를 별도 socket request로 구현하면 사이에 process가 바뀔 수 있다.
Bridge mutex는 Herdr와 OS process lifecycle을 잠그지 못한다. `terminal_id`, `revision`,
`state_change_seq`는 native session incarnation token의 대체물이 아니다. 특히 동일 terminal에서
같은 종류의 agent 재시작을 구별하지 못한다.

필요한 Herdr 변경은 live binding 발급, expected binding 검사, queue 안의 stale command 취소다.
text 전달 후 Enter 이전에 binding이 바뀌면 후속 byte 전송을 멈추고 `delivery_unknown`으로 보고한다.
사용자가 별도로 소유한 process는 언제든 종료할 수 있으므로 단순 check 한 번을 OS 수준의
원자적 compare-and-write로 표현하지 않는다. 안전한 수신 대상 보장 수준을 actor 테스트와 실제
process 교체 테스트로 입증하기 전에는 write capability를 활성화하지 않는다.

### B2 — direct attach는 부적합하지만 passive snapshot은 지원됨

후속 source/runtime 조사로 최초 결론을 수정했다. raw PTY byte stream observer는 없지만,
첫 slice의 Terminal mirror에 raw stream이 반드시 필요한 것은 아니다.

`pane.read`에 `source: visible`, `format: ansi`, `strip_ansi: false`를 보내면 현재 화면을
읽는다. 실제 Herdr 0.9.1에서 38줄 ANSI frame을 받았다. `src/app/api_helpers.rs`의
`read_terminal_snapshot`은 이 조합을 `visible_ansi()`로 처리한다.
`src/server/headless.rs`의 `alt_screen_read_spec`은 text+recent 계열에서만 interactive
history collection을 수행하므로 visible+ansi는 자동 scroll input도 유발하지 않는다.

Bridge는 frame을 주기적으로 읽어 client에 교체용 snapshot으로 전달할 수 있다.
연결마다 attach owner를 잡지 않고 resize/resume/start를 호출하지 않는다. 이를 raw output
stream 또는 historical scrollback 보장으로 표현하지 않는다. intermediate frame은 생략될 수
있으며 semantic transcript event의 lossless replay와 별개다.

따라서 별도 observer API 확장은 첫 slice의 선행 조건에서 제거한다. `TerminalSnapshot`
gateway와 진단에 실제 read 경로를 구현했다. xterm.js renderer 및 input은 아직 구현 전이며,
interactive fallback의 write는 여전히 B1을 해결해야 한다.

### B3 — 현재 native hook 미설치

현재 Claude의 identity는 read-only로 검증했지만 Herdr association은 비어 있다.
hook 설치만으로 기존 session identity가 즉시 생긴다고 가정하지 않는다. 실제 native metadata를
Herdr에서 검증하여 연결하거나, 기존 process의 지원되는 session report 경로를 검증해야 한다.
새 process/session을 생성해서 acceptance를 대체하지 않는다.

### Gate 결론

현재 설치 버전만으로 요청한 모든 invariant를 보장하는 write-enabled vertical slice는 만들 수 없다.
UI를 먼저 만들어 성공한 것으로 보이게 하지 않는다. B1과 B3는 Herdr 변경 및 기존 runtime에 대한
검증을 필요로 한다. B2의 read path는 기존 API로 해결 가능하다고 확인했다. 이 조사에서는 Herdr 수정·업데이트·재시작, agent prompt/interrupt,
integration 설치, 새 session 생성 모두 수행하지 않았다.

추천: Herdr의 조건부 입력 capability 확장을 선행 — 이유는 runtime owner 안에서 입력과
관찰 경계를 바로잡는 것이 Bridge의 PID polling/mtime guessing/private attach 우회보다 요구사항에 맞기 때문이다.


## 2026-09-23 — Herdr capability 확장과 실제 handoff

첫 조사 시의 B1/B3는 stock Herdr `0.9.1`을 기준으로 한다. `herdr` repo의
`codex/mobile-binding` branch에는 `agent.binding`과 `agent.bound_input`이 추가됐다.
macOS Claude foreground process의 시작 시각과 PID별 native session metadata를 대조해
session token을 발급한다. Herdr는 입력 queue에서 text 및 Enter byte를 쓰기 직전에도
binding을 재검증한다. metadata와 process가 불명확한 경우 입력을 거부한다.

`just ci`의 3,463개 테스트, release build와 API schema/문서 검증을 통과했다.
실행 중 Herdr `0.9.1` server를 live handoff로 교체한 후 기존 두 Claude PID,
native session ID, shell PID가 유지됐다. handoff는 새 terminal ID를 발급했다.
모바일 Chat 요청 1건은 같은 native transcript에 user/assistant record로 각각 한 번 기록됐고
PC pane에도 응답이 표시됐다. 동일 `command_id` 재전송은 파일 크기와 message 개수를
변경하지 않았다. 잘못된 binding은 Bridge와 Herdr 양쪽에서 거부됐다.

Bridge 재시작 후 두 PID가 살아 있었고 새 epoch의 snapshot에서 기존 메시지를 모두
복구했다. 오래된 epoch로 재접속한 WS에는 새 snapshot이 전달됐다. Terminal Esc 입력도
같은 Claude process로 전달됐다. Chat/Terminal 전환은 handoff 이후 pane geometry를
변경하지 않았다. Herdr live handoff 자체는 desktop client가 끊긴 동안 전체 pane layout을
기본 120×40으로 바꿨다. 재접속 시 desktop client가 크기 소유권을 다시 갖는다.

`agent.get`의 `agent_session`은 hook 미설치로 비어 있었으나 새 binding API는 실제 PID
metadata에서 native ID를 검증해 두 session을 서로 다른 transcript에 연결했다.
현재 확장은 macOS Claude에 한정된다. Codex Desktop JSONL 조사 결과를 Codex CLI
Herdr session adapter의 검증으로 간주하지 않는다.

## 2026-09-24 — 격리 Herdr Codex CLI 조사

별도 이름의 Herdr 테스트 server와 빈 workspace에서 Codex CLI `0.155.1`을 실행했다.
Herdr Codex `SessionStart` hook은 처음에 Codex의 trust review를 요구했다. 등록된
로컬 Herdr hook 명령을 검토하고 신뢰한 뒤 한 번의 도구 없는 prompt를 보냈다.
Herdr `agent.get`에는 `source: herdr:codex`, `kind: id`인 native session ID가 기록됐고,
그 ID와 정확히 일치하는 rollout JSONL 파일이 하나 생성됐다. 독립 테스트 server와
hook 등록은 조사 후 제거했고 기존 Codex 설정은 백업과 byte-identical하게 복원했다.

실제 CLI rollout은 `session_meta`, `response_item`, `event_msg`, `turn_context`,
`world_state`, `token_usage_record`를 포함했다. `session_meta.payload.id`는 Herdr가
보고한 native ID와 일치했고 `source`는 `cli`였다. `response_item`의 `user` 역할만으로
human prompt를 판별할 수 없다. 시작 시 주입된 `AGENTS.md`와 환경 정보도 `user`
record였으며 `internal_chat_message_metadata_passthrough.content_item_kinds`가
각각 `agents_md.instructions`, `environments.environment_context`로 표시했다.
실제 사용자가 보낸 입력은 `user.text`로 표시됐다. assistant text는
`response_item.payload.content[].type: output_text`에서 관찰됐다.
`event_msg`의 `item_completed` 및 `task_complete`에도 관련 정보가 있으므로
양쪽을 모두 Chat 메시지로 변환하면 중복될 수 있다.

이 조사는 transcript parser의 근거지만 안전한 Codex write binding의 증명은 아니다.
현재 Herdr 조건부 입력은 Claude process의 PID별 native metadata를 사용한다.
Codex hook session ID가 현재 foreground process incarnation과 결합돼 있는지와
같은 process에서 `/clear` 또는 resume할 때 binding이 어떻게 바뀌는지는 별도 검증이
필요하다. 이 경계가 확인되기 전에는 Codex prompt를 pane ID만으로 보내지 않는다.
