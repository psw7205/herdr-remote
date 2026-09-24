# Herdr Mobile Chat Backlog

2026-09-24 기준. 요구와 결정의 기준은 `docs/prd.md`, `docs/adr.md`이고, 확인된 구현·runtime 근거는 `docs/implementation-plan.md`, `docs/integration-findings.md`에 있다. 이 목록은 남은 작업의 순서와 완료 기준을 관리한다. 세부 구현 방식은 착수 시 실제 Herdr/API/agent version을 다시 확인한다.

현재 **Claude Code / macOS의 첫 vertical slice는 로컬에서 검증됐다**. 기존 session 발견, native transcript Chat, 동일 PTY prompt, transcript 응답, retry 중복 방지, stale binding 거부, Bridge 재시작 복구, Terminal fallback이 동작했다. Tailnet HTTPS의 실제 모바일 경로와 Codex write 경로는 완료되지 않았다.

상태: `blocked`는 외부 조치가 필요한 항목, `todo`는 미착수 또는 검증 미완료, `optional`은 핵심 handoff 이후 기능이다. 우선순위는 의존성과 session integrity를 기준으로 한다.

## P0 — 배포와 핵심 안정성

| ID | 작업 | 현재 근거·의존 관계 | 완료 기준 |
| --- | --- | --- | --- |
| P0-01 | Tailscale Serve 활성화 | `blocked`. 현재 tailnet에서 Serve가 비활성화돼 있다. 관리자 로그인·활성화는 사용자가 직접 해야 한다. Bridge는 localhost `launchd` 서비스로 실행 중이다. | Tailnet HTTPS URL이 Bridge를 proxy하며 Funnel은 꺼져 있다. |
| P0-02 | 실제 mobile Tailnet handoff | `todo`, P0-01 이후. localhost 브라우저에서만 end-to-end를 검증했다. | 소유자 mobile device에서 기존 Claude 대화와 Terminal을 열고 prompt→같은 native transcript 응답을 확인한다. HTTPS와 WebSocket reconnect가 동작하고, 다른 사용자 identity와 잘못된 Origin은 거부된다. |
| P0-03 | Tailnet network 접근 최소화 | `todo`. Bridge의 `Tailscale-User-Login` 검증은 적용됐지만 network 계층의 ACL/grant는 Bridge host로 좁히지 않았다. | 기존 다른 서비스 접근을 보존하면서 이 host의 Serve HTTPS 접근을 의도한 본인 device로 제한한 ACL/grant를 검증한다. 전역 tailnet 정책 변경 전 영향 범위를 확인한다. |
| P0-04 | Herdr patch 유지·업데이트 경로 | `todo`. 조건부 입력은 `herdr` repo의 `codex/mobile-binding` branch와 설치된 patched binary에 의존한다. stock `0.9.1`에는 API가 없고 두 binary의 version 문자열이 같다. `/api/sessions`의 `herdr.conditional_input`(`supported`/`unsupported`/`unknown`), `doctor`의 `conditional_input`·blocker, Web UI 안내로 감지를 노출하며 Bridge는 fail closed한다. build·백업·설치·live handoff·rollback·upgrade 절차는 [Herdr patch runbook](herdr-patch.md)에 기록했다. stock server에서의 `unsupported` 판정, rollback, 새 release rebase는 격리 환경에서 실행하지 않았다. P0-07과 연결된다. | Herdr 업데이트/재시작 후 `agent.binding`·`agent.bound_input` 제공 여부를 검사하고, 불일치 시 Bridge가 fail closed한다. patch 적용과 원본 binary 복구 절차를 문서화·검증한다. |
| P0-05 | 실제 pane/session 교체 회귀 테스트 | `todo`. actor 단위 테스트와 live stale-token 거부는 통과했다. A 종료→같은 pane shell/B 과정은 기존 사용자 agent를 건드리지 않기 위해 실측하지 않았다. | 격리 Herdr 환경에서 A→shell, A→B, 같은 process의 native session 교체, queued text→Enter 사이 교체를 재현한다. 오래된 Chat/Terminal command가 새 대상에 입력되지 않고 `delivery_unknown`을 중복 retry하지 않는다. |
| P0-06 | 실제 interrupt·blocked interaction | `todo`. Terminal Esc 전달과 API/unit 경로는 검증했다. 작업 중 interrupt와 permission/question 대기 화면은 실측하지 않았다. | 격리 agent에서 Stop이 기존 process에만 전달되고, 구조를 모르는 CLI 대기는 Chat이 임의 Allow/Deny를 만들지 않고 같은 pane의 Terminal에서 처리된다. 연결 종료는 agent를 중단하지 않는다. |
| P0-07 | Binding·capability 손실과 agent 종료 구분 | `todo`, P0-04와 연결. Herdr가 binding을 제공하지 않으면 `internal/session/registry.go`가 session ID를 `claude:<native-id>`에서 `pane:<pane-id>`로 바꿔 등록한다. 기존 ID는 발견되지 않은 것으로 처리돼 `lifecycle: ended`가 기록되므로, 열린 Chat은 agent가 계속 실행 중이어도 `ended`로 보인다. stock binary로 교체하거나 rollback할 때 재현될 것으로 예상한다. | Capability 손실이나 binding 손실을 agent lifecycle 종료로 표시하지 않는다. PRD §8의 `ended/unavailable`은 process 종료가 확인된 경우에만 표시한다. |
| P0-08 | Tailnet 설정 자동 감지·진단 | `todo`. `-tailnet-host`·`-tailnet-login` 기본값 `auto`, Origin 자동 추가, `doctor`의 `tailnet` section은 구현됐다. 실제 Serve·mobile 확인은 P0-01/P0-02를 기다린다. 알려진 한계: host·login은 시작 시 한 번만 해석한다. 예를 들어 Tailscale이 Running이 되기 전에 `launchd`로 부팅된 Bridge는 재시작 전까지 localhost 전용이며, background 재해석은 구현하지 않았다. 원격 경로는 Tailscale Serve만 유지한다(ADR-024). | 새 package `internal/tailnet`이 `tailscale.com` 의존성 없이 CLI를 shell-out한다. `-tailnet-host`·`-tailnet-login` 기본값 `auto`는 시작 시 한 번 `tailscale status --json`에서 host(`Self.DNSName`의 trailing dot 제거)와 node 소유 사용자의 login을 얻는다. `-tailnet-host off`는 Tailnet 요청을 받지 않는 localhost 전용이고, 명시 값은 `auto`보다 우선한다. tagged node이거나 login이 없으면 host는 유지하고 login은 비워 모든 tailnet 요청을 거부한다. CLI 없음·실행 실패·Tailscale 미실행이면 `auto` host는 localhost 전용으로 동작하고, host를 명시했는데 `auto` login만 실패하면 host를 유지한 채 모든 tailnet 요청을 거부한다. 어느 쪽이든 Bridge는 종료하지 않는다. `-tailscale-bin`은 CLI 경로를 명시하고, 없으면 PATH, 이어서 알려진 설치 경로를 찾는다(`launchd`의 최소 PATH, macOS app bundle wrapper 대응). host와 owner login이 모두 확정된 경우에만 `https://<host>`를 Origin allowlist에 자동 추가한다. `-origins`는 기본값을 대체하므로 localhost 접속이 필요하면 `http://127.0.0.1:8787`도 직접 포함한다. login이 없는 동안 `-origins`의 tailnet HTTPS Origin은 경고 log와 함께 제외한다. 시작 log에 확정된 host/login과 각각의 출처를 남긴다. `doctor`에 `tailnet` section(`cli_available`, `backend_state`, `host`, `login`, `https_certificates`, `serve_proxy`, `funnel`, 조치 방법을 담은 `issues`)을 추가한다. `serve_proxy`는 Serve가 `<host>:443`을 Bridge listen 주소로 proxy하는지 확인한다. Funnel 감지는 모든 port와 `--bg` 없이 실행한 foreground 설정을 포함하며 활성화는 top-level blocker이고, Tailnet 미설정은 localhost 전용이 지원 범위이므로 blocker가 아니다. README는 설치자 기준 절차를 먼저 안내한다. 남은 완료 기준: 소유자가 Serve를 활성화한 뒤 `doctor`의 `tailnet.issues`가 비어 있음을 확인하고 P0-01/P0-02 흐름으로 실제 접속을 검증한다. |
| P0-09 | 공개 전 점검 | `todo`, 소유자 주도. P0-01~P0-08 이후. GitHub에 공개해 다른 사용자가 자신의 tailnet에 설치하기 전 단계다. | (1) Host/Origin/identity 경계, command receipt, fixture, log를 포함한 전체 security review를 완료한다. (2) 모든 tracked 문서에서 개인·host 전제를 제거한다. 예: PRD §22의 "이 호스트처럼", P0-01/P0-03과 `docs/implementation-plan.md`·`docs/herdr-patch.md`에 적힌 이 host·tailnet의 Serve/ACL 상태. (3) 공개 전에 Git history를 재생성한다. (4) 소유자가 원하면 LICENSE와 기여 안내를 추가한다. |

## P1 — 핵심 UX와 다음 agent

| ID | 작업 | 현재 근거·의존 관계 | 완료 기준 |
| --- | --- | --- | --- |
| P1-01 | Codex 조건부 runtime binding | `todo`. 격리 Codex CLI에서 Herdr hook의 native ID와 rollout을 실측했다. 현재 Herdr binding은 macOS Claude의 PID별 metadata만 검증한다. | Codex native ID를 현재 foreground process incarnation에 결합한다. `/clear`, resume, A→B, 같은 cwd의 두 session에서 stale write를 거부한 후에만 mobile prompt를 활성화한다. |
| P1-02 | Codex transcript Chat adapter | `todo`, P1-01의 write gate와 독립적으로 parser를 검증할 수 있다. 실제 CLI rollout의 `user` 역할에는 시작 지침도 들어간다. | `session_meta.payload.id`로 파일을 정확히 찾고 `content_item_kinds: user.text`만 사람 입력으로 표시한다. assistant `output_text`를 표시하며 `event_msg` 중복을 제거한다. partial/replace/reconnect 테스트와 같은 PTY round-trip을 통과한다. |
| P1-03 | Session 목록의 attention·activity | `todo`. 현재 목록은 상태 점과 제목을 표시하지만 PRD §7의 Needs Attention/Working/Recent 묶음, 마지막 activity·메시지는 없다. | Herdr의 신뢰 가능한 상태로 그룹과 마지막 activity를 표시한다. `completed` turn과 process `ended/unavailable`를 구분하고, cwd가 같은 session도 혼동 없이 선택할 수 있다. |
| P1-04 | Markdown/code 읽기 편의 | `todo`. Markdown·표·code block은 표시되며 외부 이미지는 자동 로드하지 않는다. code copy와 긴 출력 탐색은 없다. | 모바일에서 code block 복사, 긴 표/코드 가로 스크롤, text selection이 정상 동작한다. 링크·이미지로 transcript 내용이 자동 외부 전송되지 않는다. |
| P1-05 | Terminal mobile 조작·화면 크기 | `todo`. 현재 `pane.read` visible ANSI frame을 xterm.js에 교체 표시하고 PTY resize는 하지 않는다. xterm 로컬 grid는 120×40 고정이다. | 실제 phone에서 IME, Esc/Tab/arrows/Ctrl-C/Enter, horizontal scroll을 검증한다. Herdr/desktop의 PTY size ownership을 유지하고 다른 process로 바뀌면 frame/input을 거부한다. |
| P1-06 | PWA 실제 기기 복구 | `todo`, P0-02 이후. manifest, 192/512 PNG, service worker와 foreground reconnect는 구현했다. | iOS/Android의 홈 화면 설치, safe area, keyboard viewport, 화면 잠금·네트워크 전환 후 transcript 복구를 실기기에서 확인한다. offline shell은 대화가 없을 때 agent가 계속 실행 중임을 정확히 안내한다. |
| P1-07 | Assistant live 표현 범위 결정 | `todo`. 현재 완성된 native JSONL text record와 상태 변화를 전송한다. 안정적인 streaming delta는 확인되지 않았다. | 실제 Claude/Codex CLI version에서 transcript delta 유무를 측정한다. 안정적 source가 있으면 중복 없이 표시하고, 없으면 complete message + 작업 상태로 명확히 표현한다. |
| P1-08 | Herdr live handoff geometry | `todo`. patch 설치 handoff는 Claude PID·native ID를 보존했으나 desktop client가 끊긴 동안 pane geometry가 161×45에서 기본 120×40으로 바뀌었다. | Herdr update/handoff 전후 desktop size owner가 다시 붙기 전까지도 pane 크기가 불필요하게 바뀌지 않거나, 변경·복구 조건이 사용자에게 명확하다. 모바일 Terminal이 resize하지 않는 불변식은 유지한다. |

## P2 — 필요가 확인될 때 추가할 기능

| ID | 작업 | 착수 조건·결정 경계 | 완료 기준 |
| --- | --- | --- | --- |
| P2-01 | Tool activity card | 실제 transcript에서 안정적인 tool 시작/완료 식별자가 확인될 때. | 텍스트 대화를 중복하지 않는 compact card와 상세 보기. parsing 실패 시 기본 Chat/Terminal 유지. |
| P2-02 | Structured permission/question | agent가 신뢰 가능한 structured request/response contract를 제공할 때. | capability별 UI만 노출하고 side effect는 native request에 정확히 연결한다. 불확실한 상태는 Terminal fallback. |
| P2-03 | Changed Files·diff | ADR-016. Git은 read-only source이며 동일 worktree의 사용자 변경을 특정 agent에 귀속하지 않는다. | modified/added/deleted, diff, line count 표시. source edit/commit/push UI 없음. |
| P2-04 | File/image attachment | ADR-015의 범위를 확정할 때. | host 전용 임시 directory, random filename, traversal/symlink 차단, 크기 제한, TTL/cleanup, agent가 읽을 수 있는 path 전달을 검증한다. |
| P2-05 | Notifications | ADR-017. 실제 mobile PWA push 제약과 외부 push infrastructure를 확인한 뒤. | `needs_attention`/`completed`에서 파생하고 민감한 prompt/output을 payload에 싣지 않는다. 전달 실패 후 앱에서 완전 복구된다. |
| P2-06 | Receipt·event buffer 운영 | 명령량/장기 실행에서 크기 문제가 관찰될 때. | command ID 만료 뒤 재수락 금지를 유지하며 disk retention을 설계한다. replay buffer miss는 언제나 native snapshot으로 복구한다. conversation DB/Redis/broker를 추가하지 않는다. |
| P2-07 | Client preference·검색 | 첫 handoff UX에서 필요가 확인될 때. | 읽음 위치, 즐겨찾기 또는 session 검색은 presentation state로만 저장하고 native transcript를 복제하지 않는다. |

## 계속 범위 밖인 항목

Multi-host, multi-user 계정/조직, public internet 배포, 자체 agent runner, conversation DB,
full IDE·source editor, mobile Git commit/push/merge는 현 PRD의 non-goal이다.
이 목록에서 우선순위를 받은 기능이 아니다.

**의도:** 기존 Herdr session의 무결성과 모바일에서의 복구 가능성을 먼저 완성한다.
**추천:** P0-08로 Tailnet 설정을 자동화한 뒤 P0-01→02의 실제 Tailnet handoff를 닫고,
P0-03~07을 격리 환경에서 검증한다. 이어서 P0-09 공개 전 점검을 거쳐 P1-01/02 Codex adapter로
확장한다 — 이유는 remote 사용 경로와 잘못된 process 입력 방지가
Rich Chat 기능보다 먼저 증명돼야 하기 때문이다.
