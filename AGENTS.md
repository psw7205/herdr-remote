# Agent 작업 지침

## 시작 전

- 응답과 새 문서는 한국어로 작성한다. code, identifiers, API 이름은 English를 유지한다.
- `README.md`로 실행 조건을, `docs/backlog.md`로 현재 범위를 확인한다.
- 요구와 architecture 결정은 `docs/prd.md`, `docs/adr.md`를 기준으로 한다. 구현 사실은 code와 runtime에서 검증한다.
- 문서 배치와 갱신 규칙은 `docs/README.md`를 따른다.
- Herdr 연동을 바꾸기 전 `docs/records/integration-findings.md`를 읽는다. 과거 조사 결과를 현재 설치 버전의 지원 여부로 단정하지 않는다.
- `git status --short`와 현재 branch를 확인한다. 기존 사용자 변경을 보존하고 요청 범위만 수정한다.
- 검토·분석 요청은 read-only다. 구현 요청에는 성공 기준과 관련 검증을 먼저 정한다.

## 제품 불변식

- Herdr가 pane, PTY, agent process와 lifecycle의 owner다. Bridge는 agent를 직접 실행하거나 기존 session을
  resume하지 않는다. 새 session은 사용자의 명시적 요청이 있을 때 Herdr 공개 API(`workspace.create`·`tab.create`·
  `agent.start`)로만 요청하며, agent 인자와 임의 경로는 받지 않는다(`docs/adr.md` ADR-037).
- Chat history의 source of truth는 native transcript다. transcript에는 쓰지 않는다.
- prompt, interrupt, raw input은 현재 Herdr `runtime_binding`을 검증하는 경로만 사용한다. 미지원 API를 raw pane input으로 우회하지 않는다.
- 같은 `command_id`는 한 번만 dispatch한다. timeout이나 crash는 미전달의 증거가 아니며 `delivery_unknown`을 자동 재전송하지 않는다.
- command receipt는 conversation persistence가 아니다. receipt 삭제로 오래된 command ID를 다시 수락하게 만들지 않는다.
- snapshot cursor와 replay/live 등록의 원자성을 유지한다. Bridge restart나 transcript resync는 epoch를 바꾼다.
- browser/WS 종료는 agent interrupt, pane close, session close를 유발하지 않는다.
- Terminal은 같은 pane의 visible ANSI snapshot을 표시한다. mobile viewport로 Herdr PTY를 resize하거나 private attach의 takeover/resume을 호출하지 않는다.
- 신뢰 가능한 structured source가 없는 permission/question은 Terminal fallback으로 처리한다.
- localhost bind, exact Host/Origin 검사, Tailscale Serve 소유자 identity 검증을 유지한다. Funnel과 wildcard CORS는 지원 범위가 아니다.
- native transcript 원문, 실제 prompt, binding token을 fixture·로그·Git 문서에 복사하지 않는다. fixture는 실제 구조를 확인한 익명 sample로 작성한다.
- 실제 hostname, tailnet 이름, IP, 계정, 기기 식별자는 Git 문서에 `<tailnet-host>` 같은 placeholder로 쓴다. 실제 host의 ACL 범위, 열린 port 같은 보안 상태는 기록하지 않고 필요한 조치만 일반 조건으로 backlog에 남긴다.
- conversation DB, Redis, broker, 별도 agent runner는 추가하지 않는다.

## 코드 경계

| 경로 | 책임 |
| --- | --- |
| `cmd/bridge/`, `cmd/doctor/` | server 진입점, read-only 진단 |
| `internal/herdr/` | 검증된 Herdr socket API |
| `internal/session/` | discovery, binding, lifecycle, adapter 연결 |
| `internal/claude/`, `internal/transcript/` | native history 해석, incremental file read |
| `internal/command/`, `internal/stream/` | durable receipt, snapshot/replay/live |
| `internal/httpapi/` | HTTP/WS, Origin·Host·identity 경계 |
| `internal/tailnet/` | Tailscale CLI 조회, owner·host 자동 감지. `tailscale.com` dependency 없음 |
| `web/src/`, `web/public/` | thin client, Markdown, Terminal, PWA |

Agent-specific parser나 Herdr socket logic을 browser로 옮기지 않는다. Go에서는 `net/http`,
`coder/websocket`, `fsnotify`, `log/slog`를 사용하고 frontend는 React/TypeScript/Vite를 유지한다.
Package manager는 pnpm, toolchain 기준은 `mise.toml`이다.

## Herdr 및 실제 runtime 작업

- stock Herdr `0.9.1`과 local patch는 같은 version 문자열을 사용할 수 있다. version만으로 conditional input 지원을 판단하지 않는다.
- 별도 `herdr` repo를 수정할 때는 그 repo의 지침과 변경 상태도 확인한다. 이 repo의 검증만으로 Herdr patch 검증을 대신하지 않는다.
- 설치된 binary·사용자 hook·LaunchAgent 변경은 code 변경과 구분하고, 작업 범위 안에서 backup과 복구 방법을 확보한다.
- 기존 사용자 agent를 종료하거나 새 session으로 교체해 handoff 성공을 만들지 않는다.
- lifecycle 재현이 필요하면 `CONTRIBUTING.md`의 격리 Herdr 환경을 사용하고, 생성한 테스트 자원과 설정 변경을 정리한다.
- `doctor`의 성공은 capability 관찰이다. 실제 PTY handoff나 모든 race의 통과를 의미하지 않는다.

## 검증과 완료

실행 명령과 변경별 검증 범위는 `CONTRIBUTING.md`를 따른다. 구현하지 않은 기능이나
실행하지 않은 scenario를 완료로 표시하지 않는다. 문서만 바꿀 때는 관련 링크·경로·diff를 검증한다.

검증 수준은 실패 형태로 정한다. 잘못된 process 입력, 중복 dispatch, agent가 모르게 멈추는 경우,
identity·Origin 경계처럼 조용히 실패하거나 되돌릴 수 없는 변경은 unit test와 격리 Herdr 실측을
완료 조건으로 둔다. 화면에 드러나고 새로고침·재시도로 복구되는 UI·실기기 동작은 test와 fixture
확인 뒤 backlog `in-use`로 두고 사용하면서 고친다. 실기기 확인이 없다는 이유로 다른 작업을 막지 않는다.

Architecture 책임, persistence, identity, security, protocol 의미가 바뀌면 PRD/ADR을 함께
갱신한다. 남은 작업은 `docs/backlog.md`에 기존 ID와 의존 관계를 유지하며 기록한다.

Tracked 문서에는 개인 절대 경로를 쓰지 않는다. 현재 repo의 파일은 repo-relative 경로로,
다른 repo는 repo 이름으로 참조한다. commit은 요청 범위만 stage하며 push와 main integration은
사용자의 해당 요청에 따라 수행한다. 완료 보고에는 변경, 검증, 남은 한계를 구분한다.
