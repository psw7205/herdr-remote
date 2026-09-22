# 첫 vertical slice 구현 계획

상태: write/observer integration gate에서 차단. 독립적인 읽기 전용 gateway와 진단은 구현했다.
전체 vertical slice 구현 완료를 뜻하지 않는다.

**Goal:** 기존 Herdr Claude를 발견하고 기존 transcript를 Chat으로 읽으며 같은 PTY/native session에
prompt를 보내고 응답을 transcript에서 다시 관찰한다. 새 agent/session 생성은 허용하지 않는다.

**Architecture:** Herdr는 runtime owner, transcript는 conversation source, PTY는 interactive source다.
Go Bridge 한 process에서 discovery, incremental projection, command receipt, HTTP/WS를 처리한다.
Client는 normalized event만 소비한다.

**Tech stack:** React + TypeScript + Vite, pnpm, Go `net/http`, `coder/websocket`, `fsnotify`,
`log/slog`, xterm.js, mise. conversation DB/Redis/broker 없음. command receipt만 최소 embedded persistence.

## 1. Herdr gate

선행 근거: [조사 결과](integration-findings.md). 기존 Herdr checkout은 설치 버전과 다르므로
그 checkout을 무조건 수정하거나 실행 중 server를 대체하지 않는다.

- [x] 설치 버전과 source, API schema 대조.
- [x] 실행 중 Claude process와 native transcript identity 읽기 검증.
- [ ] Herdr `src/api/schema/agents.rs`, `src/app/api/agents.rs`, `src/terminal/state.rs`,
  `src/pty/actor/unix.rs`에서 authoritative binding, expected binding, queued write invalidation 구현.
- [ ] Herdr terminal observer는 `src/server/headless.rs`의 owner attach와 별도 경로로 구현.
  public API boundary를 유지하며 private TUI protocol 복제를 피한다.
- [ ] Claude PID/native session association을 Herdr에서 검증하고 노출. main/subagent 및 same-cwd 혼동 차단.
- [ ] verify: A→shell, A→B, 동일 native session resume/new process, PID 재사용, queue 대기 중 종료,
  text/Enter 사이 교체 모두에서 successor가 stale input을 받지 않는다. 미보장 시 write는 비활성.
- [ ] verify: observer 2개 connect/disconnect, resize 변경 없음, resume/start 호출 없음, 기존 process 유지.

## 2. Bridge read path

- [x] `mise.toml`, `go.mod`: 설치된 toolchain version 고정. 아직 외부 dependency가 없어 `go.sum`은 없다.
- [x] `internal/herdr/gateway.go`: 실제 public API의 snapshot/process 조회, timeout/size/response identity 검증.
- [x] `cmd/doctor/main.go`: 기존 server의 association과 구현되지 않은 capability를 읽기 전용으로 보고.
- [x] 위 gateway/진단에 대해 `go test -race ./...`, `go vet ./...` 및 실제 Herdr 0.9.1 조회 검증.

이 기반은 아래 read path 전체 완료를 의미하지 않는다. Herdr write/observer gate와 별개로 검증했다.

예정 파일과 책임:

| 파일 | 책임 |
| --- | --- |
| `mise.toml`, `go.mod`, `go.sum` | 설치·검증한 runtime/library version 고정 |
| `cmd/bridge/main.go` | localhost bind, 설정, shutdown, slog |
| `internal/herdr/gateway.go` | 실제 공개 socket API codec, bounded timeout, capability 확인 |
| `internal/session/registry.go` | identity/binding/lifecycle 관리, discovery reconciliation |
| `internal/claude/resolve.go` | native session에서 transcript resolution, ambiguous/mismatch 거부 |
| `internal/claude/transcript.go` | 검증된 JSONL text message normalization, sidechain/duplicate 처리 |
| `internal/transcript/tailer.go` | file identity/offset/partial buffer, fsnotify 및 제한적 reconciliation |
| `internal/stream/session.go` | session별 epoch, snapshot, bounded replay, atomic subscribe |
| `internal/httpapi/server.go` | session list/snapshot/live HTTP/WS, Origin 검증 |

- [ ] 실제 sample의 **구조만** 반영한 익명 fixture 작성. 사용자 transcript 원문을 commit하지 않는다.
- [ ] parser test를 먼저 작성하고 실패를 확인한 뒤 구현한다. string user, text blocks,
  tool_result 제외, unknown records, malformed complete record, mixed session ID, sidechain 포함.
- [ ] watcher tests: partial record 완성, 여러 record 한 write, duplicate fs event,
  truncate, inode replacement, rotation, missing file, same-size rewrite 감지/재동기화.
  불확실한 rewrite를 append로 추측하지 않고 epoch를 바꿔 snapshot 재구성.
- [ ] snapshot lock과 replay/live registration lock을 공유. network write는 lock 밖에서 수행.
  subscriber overflow는 조용히 drop하지 않고 resync/close한다.
- [ ] verify: snapshot C 이후 subscribe 전에 append한 event가 정확히 한 번 보인다.
  buffer miss/old epoch/future cursor는 fresh snapshot으로 복구된다.
- [ ] verify: `go test -race ./...`, `go vet ./...`. fsnotify 유실/재연결 시 전체 파일 반복 parse 없이
  offset 기반 reconciliation으로 복구한다.

## 3. Command path

예정 파일: `internal/command/service.go`, `internal/command/receipts.go`, 각 `_test.go`.

- [ ] request는 `command_id`, `session_id`, `runtime_binding`, `command_type`, `payload`를 가진다.
  command 종류는 prompt/interrupt/terminal_input으로 분리한다.
- [ ] validated command ID와 canonical request digest를 durable receipt로 reserve한 뒤 전송한다.
  동일 ID/digest는 저장 결과, 다른 digest는 conflict. in-flight 중복도 한 번만 dispatch.
- [ ] pending 상태에서 crash하면 unknown으로 복구하고 재전송하지 않는다. receipt 저장 실패는
  전송 전에 fail closed. 보존 기간 뒤 ID를 새 command처럼 재수락하지 않도록 만료 namespace를 거부한다.
- [ ] write-time expected binding은 Herdr가 검증한다. 지원하지 않는 버전에서 raw pane input으로 대체하지 않는다.
- [ ] PTY 성공은 command accepted다. Chat user message는 transcript에서 관찰한 뒤 확정한다.
  transcript record와 command의 상관관계가 없다면 동일 text만으로 pending을 확정하지 않는다.
- [ ] verify: duplicate concurrent requests, response timeout, partial delivery, Bridge restart,
  receipt corruption/storage failure, different payload reuse, old binding 모두 테스트한다.

## 4. Mobile client / Terminal fallback

예정 파일: `web/package.json`, `pnpm-lock.yaml`, `web/vite.config.ts`, `web/tsconfig.json`,
`web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/api.ts`,
`web/src/SessionChat.tsx`, `web/src/TerminalView.tsx`, `web/src/styles.css`.

- [ ] Sessions → Chat, Markdown/code, composer, pending delivery state, connection state.
  raw HTML을 허용하지 않는 Markdown renderer를 사용하고 unsafe URL을 허용하지 않는다.
- [ ] 선택 session/binding을 고정하고 바뀌면 composer를 잠근다. draft는 session별로 유지한다.
- [ ] HTTP snapshot cursor로 WS subscribe. epoch가 바뀌면 snapshot으로 교체한다.
  reconnect와 background/foreground 전환에서 오래된 callback을 무시한다.
- [ ] xterm.js는 같은 session의 observer만 사용하고 local viewport를 Herdr resize에 전달하지 않는다.
  unsupported 상태에서 Chat composer를 제한하고 Terminal로 전환한다.
- [ ] verify: client tests로 late response/session switch/duplicate event/unknown event/reconnect 검증.
  `pnpm --dir web build` 및 browser에서 mobile viewport·IME·safe area·Markdown 표시 확인.

## 5. 실제 handoff acceptance

아래는 아직 실행하지 않았다. 테스트 mock 통과만으로 실제 완료라고 표시하지 않는다.

| 시나리오 | 통과 조건 |
| --- | --- |
| 기존 session 발견 | 실제 Herdr native binding과 transcript identity 일치 |
| 동일 cwd 여러 session | session ID 기준으로 각각 분리, ambiguous는 fail closed |
| mobile prompt | 같은 PID/process start/native session 유지, transcript user 및 assistant 기록 관찰 |
| no duplicate runtime | 요청 전후 process/session 집합에 새 agent 없음 |
| stale pane | A→shell/B 이후 오래된 command가 successor에 한 byte도 입력하지 않음 |
| retry | 같은 command ID의 동시·timeout retry가 한 번만 dispatch |
| snapshot race | C 이후 발생한 모든 event를 replay/live 또는 snapshot으로 복구 |
| disconnect | WS close가 pane/process/agent state를 종료시키지 않음 |
| Bridge restart | 기존 agent 유지, epoch 변경, unknown command 재전송 없음 |
| unsupported interaction | 임의 permission/question action 대신 동일 terminal fallback |
| security | unknown/null Origin 및 잘못된 Host 거부, mutation은 JSON + exact Origin, WS도 동일 검증 |

완료 후 실행 명령, 검증한 Herdr/Claude 버전, 실제 handoff 근거를 README에 기록한다.
현재 위 command와 파일은 구현 예정이며 아직 실행 가능한 제품으로 제공하지 않는다.

추천: gate 통과 후 이 순서로 구현 — 이유는 UI보다 session integrity를 먼저 증명해야 재작업과 잘못된 입력을 줄일 수 있기 때문이다.
