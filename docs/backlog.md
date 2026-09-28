# Herdr Mobile Chat Backlog

2026-09-28 기준. 요구와 결정의 기준은 `docs/prd.md`, `docs/adr.md`이고, 확인된 구현·runtime 근거는 `docs/records/verification.md`, `docs/records/integration-findings.md`에 있다. 이 목록은 남은 작업의 순서와 완료 기준을 관리한다. 세부 구현 방식은 착수 시 실제 Herdr/API/agent version을 다시 확인한다.

현재 **Claude Code / macOS의 첫 vertical slice는 로컬에서 검증됐다**. 기존 session 발견, native transcript Chat, 동일 PTY prompt, transcript 응답, retry 중복 방지, stale binding 거부, Bridge 재시작 복구, Terminal fallback이 동작했다. 2026-09-24에는 Funnel 없이 Tailscale Serve HTTPS를 활성화했고(P0-01), 소유자 phone에서 Tailnet HTTPS로 기존 대화 표시, prompt 한 번 전달과 같은 native session 응답, Terminal 표시를 확인했다. 화면 잠금·네트워크 전환 후 WebSocket reconnect, 다른 tailnet 사용자 거부의 실측, PWA 홈 화면 설치, Codex write 경로는 완료되지 않았다.

상태: `done (날짜)`는 완료 기준을 실측으로 확인한 항목이며 근거를 한 줄로 남긴다. `blocked`는 외부 조치가 필요한 항목, `todo`는 미착수 또는 검증 미완료, `optional`은 핵심 handoff 이후 기능이다. 우선순위는 의존성과 session integrity를 기준으로 한다.

## P0 — 배포와 핵심 안정성

| ID | 작업 | 현재 근거·의존 관계 | 완료 기준 |
| --- | --- | --- | --- |
| P0-01 | Tailscale Serve 활성화 | `done (2026-09-24)`. 소유자가 HTTPS Certificates를 활성화하고 `tailscale serve --bg 8787`을 실행했다. `doctor`는 `tailnet.https_certificates: true`, `serve_proxy: true`, `funnel: false`, `issues: []`, `blockers: []`를 보고했고, `tailscale funnel status`는 host를 tailnet only로, `/`를 `http://127.0.0.1:8787` proxy로 표시했다. `https://<tailnet-host>/api/sessions`는 200과 `herdr.conditional_input: supported`를 반환했다. | Tailnet HTTPS URL이 Bridge를 proxy하며 Funnel은 꺼져 있다. |
| P0-02 | 실제 mobile Tailnet handoff | `todo`, P0-01 이후. 2026-09-24 소유자 phone에서 Tailnet HTTPS로 기존 대화 표시, prompt 한 번 전달과 같은 native session 응답, Terminal 표시, 잘못된 Origin 거부를 확인했다. 남은 것: 다른 tailnet 사용자 identity 거부는 unit test로만 검증했고, 화면 잠금·네트워크 전환 후 WebSocket reconnect는 미검증이다. PWA 홈 화면 설치와 full screen 관찰은 P1-06에 남아 있다. [근거](records/verification.md#p0-02-실제-mobile-tailnet-handoff) | 소유자 mobile device에서 기존 Claude 대화와 Terminal을 열고 prompt→같은 native transcript 응답을 확인한다. HTTPS와 WebSocket reconnect가 동작하고, 다른 사용자 identity와 잘못된 Origin은 거부된다. |
| P0-03 | Tailnet network 접근 최소화 | `todo`. Bridge의 `Tailscale-User-Login` 검증은 적용됐지만 network 계층의 ACL/grant는 Bridge host로 좁히지 않았다. | 기존 다른 서비스 접근을 보존하면서 Bridge host의 Serve HTTPS 접근을 의도한 본인 device로 제한한 ACL/grant를 검증한다. 전역 tailnet 정책 변경 전 영향 범위를 확인한다. |
| P0-04 | Herdr patch 유지·업데이트 경로 | `todo`, P0-07과 연결. 조건부 입력은 fork `psw7205/herdr`의 `mobile-binding` branch와 설치된 patched binary에 의존하며(ADR-036), 감지 결과를 `/api/sessions`·`doctor`·Web UI로 노출하고 Bridge는 fail closed한다. 절차는 [Herdr patch runbook](herdr-patch.md)에 있다. 남은 것: stock server의 `unsupported` 판정, rollback, 새 release rebase, P0-07 scenario를 격리 환경에서 실행한다. 후속 patch 후보는 `pane.read`의 실제 PTY cols/rows다(P1-05). [근거](records/verification.md#p0-04-herdr-patch-유지업데이트-경로) | Herdr 업데이트/재시작 후 `agent.binding`·`agent.bound_input` 제공 여부를 검사하고, 불일치 시 Bridge가 fail closed한다. patch 적용과 원본 binary 복구 절차를 문서화·검증한다. |
| P0-05 | 실제 pane/session 교체 회귀 테스트 | `todo`. actor 단위 테스트와 live stale-token 거부는 통과했다. A 종료→같은 pane shell/B 과정은 기존 사용자 agent를 건드리지 않기 위해 실측하지 않았다. | 격리 Herdr 환경에서 A→shell, A→B, 같은 process의 native session 교체, queued text→Enter 사이 교체를 재현한다. 오래된 Chat/Terminal command가 새 대상에 입력되지 않고 `delivery_unknown`을 중복 retry하지 않는다. |
| P0-06 | 실제 interrupt·blocked interaction | `todo`. Terminal Esc 전달과 API/unit 경로는 검증했다. 작업 중 interrupt와 permission/question 대기 화면은 실측하지 않았다. | 격리 agent에서 Stop이 기존 process에만 전달되고, 구조를 모르는 CLI 대기는 Chat이 임의 Allow/Deny를 만들지 않고 같은 pane의 Terminal에서 처리된다. 연결 종료는 agent를 중단하지 않는다. |
| P0-07 | Binding·capability 손실과 agent 종료 구분 | `done (2026-09-24)`, P0-04와 연결. binding·capability를 잃은 `claude:<native-id>` session은 `unverified`로 남아 prompt·interrupt·Terminal을 fail closed하고, `pane:<pane-id>` item은 같은 pane에서 native session이 검증되면 `superseded`+`successor_id`로 바뀐다. 근거는 `internal/session` unit test와 `web/src/api.test.ts`이며, 실제 stock binary 실행과 App의 successor 전환 render test는 P0-04의 격리 환경 검증에 남는다. 남은 한계: `agent_session` 없이 같은 pane의 claude→claude 교체를 구분하지 못하고, agent 감지가 순간적으로 빠지면 `ended`가 된다. [근거](records/verification.md#p0-07-bindingcapability-손실과-agent-종료-구분) | Capability 손실이나 binding 손실을 agent lifecycle 종료로 표시하지 않는다. PRD §8의 `ended`는 process 종료가 확인된 경우에만 표시한다. |
| P0-08 | Tailnet 설정 자동 감지·진단 | `done (2026-09-24)`. `-tailnet-host`·`-tailnet-login` 기본값 `auto`, Origin 자동 추가, `doctor`의 `tailnet` section을 구현했고 P0-01/P0-02에서 실제 접속을 확인했다. 남은 mobile 확인은 P0-02에서 추적한다. 알려진 한계: host·login은 시작 시 한 번만 해석하며 background 재해석은 구현하지 않았다. [근거](records/verification.md#p0-08-tailnet-설정-자동-감지진단) | `auto` 기본값이 시작 시 `tailscale status --json`에서 host와 소유자 login을 얻고, 둘 다 확정된 경우에만 tailnet Origin을 추가한다. 감지가 실패해도 Bridge는 종료하지 않고 localhost 전용이나 tailnet 거부로 동작한다. `doctor`는 `tailnet` 설정 문제를 조치 방법과 함께 보고하고 Funnel·identity 우회 forward를 blocker로 둔다. 세부 규칙은 [ADR-024](adr.md#owner-자동-감지-기본값)와 [완료 기준 원문](records/verification.md#p0-08-tailnet-설정-자동-감지진단)을 따른다. |
| P0-09 | 공개 전 점검 | `done (2026-09-28)`. `psw7205/herdr-remote`를 public으로 공개하고 private vulnerability reporting을 켰다. 2026-09-28에 공개 전 review의 결함 수정([test 근거](records/verification.md#공개-전-보안-수정-검증-2026-09-28)), LICENSE와 Go module 경로, history 치환, `herdr` patch branch 공개(ADR-036), 문서 review를 마쳤다. [근거](records/verification.md#p0-09-공개-전-점검) | (1) Host/Origin/identity 경계, command receipt, fixture, log를 포함한 전체 security review를 완료한다. (2) 모든 tracked 문서에서 개인·host 전제가 남지 않았는지 다시 확인한다. (3) 공개 전에 Git history에서 host 보안 상태 서술을 제거하고, `refs/codex/*` 같은 tool snapshot ref는 push 대상에서 제외한다. (4) LICENSE를 둔다. (5) README·runbook이 참조하는 `herdr` patch branch를 public fork에 공개한다. |

## P1 — 핵심 UX와 다음 agent

| ID | 작업 | 현재 근거·의존 관계 | 완료 기준 |
| --- | --- | --- | --- |
| P1-01 | Codex 조건부 runtime binding | `todo`. 격리 Codex CLI에서 Herdr hook의 native ID와 rollout을 실측했다. 현재 Herdr binding은 macOS Claude의 PID별 metadata만 검증한다. | Codex native ID를 현재 foreground process incarnation에 결합한다. `/clear`, resume, A→B, 같은 cwd의 두 session에서 stale write를 거부한 후에만 mobile prompt를 활성화한다. |
| P1-02 | Codex transcript Chat adapter | `todo`, P1-01의 write gate와 독립적으로 parser를 검증할 수 있다. 실제 CLI rollout의 `user` 역할에는 시작 지침도 들어간다. | `session_meta.payload.id`로 파일을 정확히 찾고 `content_item_kinds: user.text`만 사람 입력으로 표시한다. assistant `output_text`를 표시하며 `event_msg` 중복을 제거한다. partial/replace/reconnect 테스트와 같은 PTY round-trip을 통과한다. |
| P1-03 | Session 목록의 attention·activity | `done (2026-09-25)`. 목록을 입력 필요/작업 중/완료/대기/기타로 묶고, 묶음 안에서는 최근 activity 순으로 보인다. Bridge는 목록 응답에 additive `last_activity`·`last_message`(transcript의 가장 최근 보이는 message, 160 rune preview)를 싣는다. 근거: `internal/session` registry test, `web/src` presentation·목록 test, fixture screenshot. 개발용 Bridge로 실제 session 3개(같은 project 2개 포함)를 read-only로 확인했다([검증 기록](records/verification.md#uiux-개편-검증-2026-09-25)). | Herdr의 신뢰 가능한 상태로 그룹과 마지막 activity를 표시한다. `completed` turn, binding을 잃은 `unverified`, 식별 전 `unbound`, process `ended`를 구분하고, cwd가 같은 session도 혼동 없이 선택할 수 있다. |
| P1-04 | Markdown/code 읽기 편의 | `todo`. 2026-09-25 code block 복사(언어 label, 결과 표시), 표·code block의 개별 가로 scroll, code의 기계 번역 제외를 구현했다(render test, fixture screenshot). 외부 이미지는 계속 자동 로드하지 않는다. 남은 것: 실기기의 복사와 text selection 확인. | 모바일에서 code block 복사, 긴 표/코드 가로 스크롤, text selection이 정상 동작한다. 링크·이미지로 transcript 내용이 자동 외부 전송되지 않는다. |
| P1-05 | Terminal mobile 조작·화면 크기 | `todo`. `pane.read` visible ANSI frame을 xterm.js에 표시하고 Herdr PTY resize는 호출하지 않는다. grid는 Bridge가 `pane.layout`에서 넣는 additive `cols`·`rows`(ADR-035)와 frame 크기 중 큰 값이고, 화면 폭 fit·글자 크기 버튼·keyboard 대응은 headless Chrome에서 확인했다. 남은 것(실제 phone 미검증): iOS keyboard와 key bar, pinch, 6px 가독성, IME, Esc/Tab/arrows/Ctrl-C/Enter, horizontal scroll. Herdr patch 권고(P0-04 범위): `pane.read`에 같은 lock에서 읽은 PTY cols/rows를 넣는다. keyboard 보정은 P1-09로 옮겼다. [근거](records/verification.md#p1-05-terminal-mobile-조작화면-크기) | 실제 phone에서 IME, Esc/Tab/arrows/Ctrl-C/Enter, horizontal scroll을 검증한다. Herdr/desktop의 PTY size ownership을 유지하고 다른 process로 바뀌면 frame/input을 거부한다. |
| P1-06 | PWA 실제 기기 복구 | `todo`, P0-02 이후. manifest, 192/512 PNG, service worker와 foreground reconnect를 구현했고, 2026-09-28부터 cache 이름은 build 산출물 hash로 자동 갱신된다. 2026-09-24 phone에서 full screen이 아니었던 관찰은 browser tab 실행이 가장 유력한 원인이다. 남은 것(실기기): 홈 화면 설치, 화면 잠금·네트워크 전환 후 복구, 설치된 PWA full screen, 이미 설치한 PWA의 icon 갱신. [근거](records/verification.md#p1-06-pwa-실제-기기-복구) | iOS/Android의 홈 화면 설치, safe area, keyboard viewport, 화면 잠금·네트워크 전환 후 transcript 복구를 실기기에서 확인한다. offline shell은 대화가 없을 때 agent가 계속 실행 중임을 정확히 안내한다. |
| P1-07 | Assistant live 표현 범위 결정 | `todo`. 현재 완성된 native JSONL text record와 상태 변화를 전송한다. 안정적인 streaming delta는 확인되지 않았다. | 실제 Claude/Codex CLI version에서 transcript delta 유무를 측정한다. 안정적 source가 있으면 중복 없이 표시하고, 없으면 complete message + 작업 상태로 명확히 표현한다. |
| P1-08 | Herdr live handoff geometry | `todo`. patch 설치 handoff는 Claude PID·native ID를 보존했으나 desktop client가 끊긴 동안 pane geometry가 161×45에서 기본 120×40으로 바뀌었다. | Herdr update/handoff 전후 desktop size owner가 다시 붙기 전까지도 pane 크기가 불필요하게 바뀌지 않거나, 변경·복구 조건이 사용자에게 명확하다. 모바일 Terminal이 resize하지 않는 불변식은 유지한다. |
| P1-09 | 화면 틀·scroll·navigation | `todo`, 실기기 확인만 남음. 2026-09-25에 화면 틀을 visual viewport에 고정해 대화 영역만 scroll하고, Back이 Terminal → Chat → 목록 순서로 돌아가게 했다(headless 360·390·412폭 확인). 남은 것: Android 실기기 keyboard와 설치형 PWA, iOS. P0-02와 같은 실기기 세션에서 확인한다. [근거](records/verification.md#p1-09-화면-틀scrollnavigation) | 실기기에서 keyboard를 열어도 header와 composer가 보이고, Back이 Terminal → Chat → 목록 순서로 돌아간다. |
| P1-10 | Chat 내용 정리 | `done (2026-09-25)`. Claude adapter는 `isMeta` 기록, slash command·bash mode의 호출과 출력, background 작업 알림을 Chat message에서 빼고 graph node로만 남긴다. `internal/claude` test와 실제 transcript 비교·dry-run으로 확인했다. `<system-reminder>` block과 synthetic `No response requested.`는 실제 구조를 확인하지 못해 규칙에 넣지 않았다. [근거](records/verification.md#p1-10-chat-내용-정리) | 사용자가 입력하지 않은 기록이 "나" 메시지로 보이지 않고, 사용자의 실제 입력은 모두 보인다. |
| P1-11 | Design foundation | `done (2026-09-25)`. `light-dark()` color token, system font, Lucide icon, 화면별 CSS Module로 바꿨고, 2026-09-27에 배색을 cool graphite로 바꿨다. 근거는 dev 전용 fixture page(`web/fixture.html`) screenshot과 Web Interface Guidelines audit이다. [근거](records/verification.md#p1-11-design-foundation) | component는 token만 쓰고, 두 theme과 reduced motion에서도 상태 표시가 구분된다. |
| P1-12 | transcript 이동 뒤 경로 재해석 | `todo`. 2026-09-25 관찰: Claude Code session이 worktree로 working directory를 옮기자, transcript 파일이 다른 project directory로 옮겨지고 원래 파일은 사라졌다. 실행 중인 Bridge는 discovery 때 찾은 경로를 계속 watch해서, 그 session의 Chat이 이동 시점 이후로 갱신되지 않았다. 새로 시작한 Bridge는 새 경로를 찾았다. | transcript 파일의 이동이나 삭제를 감지하면 native session ID로 경로를 다시 찾고 새 epoch snapshot으로 복구한다. 입력 보호와 Terminal에는 영향이 없다. |
| P1-13 | 장기 실행 Bridge의 resource 상한 | `todo`. 2026-09-28에 `ended`·`superseded` item의 transcript watcher를 닫아 본 적 있는 session 수에 비례하던 fd 증가를 없앴다(`internal/session` test). 남은 한계: macOS kqueue는 감시 directory의 파일마다 fd를 열어 active session 수 × project directory 파일 수만큼 fd를 쓴다. 시작 시 `fsnotify.NewWatcher`가 실패하면 재시도하지 않는다. 종료된 item도 message·ring·projection을 계속 보유한다. `doctor`는 `Services[*].TCP` forward를 검사하지 않고, MagicDNS host를 얻지 못하면 Serve 상태를 읽지 않는다. | 수 주 실행해도 fd와 memory가 active session 수에만 비례하고, watcher 생성 실패는 재시도나 명확한 오류로 드러난다. |

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
| P2-08 | Local command의 compact 표시 | slash command 호출과 결과를 대화에 남길 필요가 확인될 때. 지금은 P1-10에 따라 숨긴다. | PRD §15 Status Event처럼 대화를 방해하지 않게 작게 표시한다. 이를 위한 event는 ADR과 함께 추가하고, 사용자 입력과 구분한다. |

## 계속 범위 밖인 항목

Multi-host, multi-user 계정/조직, public internet 배포, 자체 agent runner, conversation DB,
full IDE·source editor, mobile Git commit/push/merge는 현 PRD의 non-goal이다.
이 목록에서 우선순위를 받은 기능이 아니다.

**의도:** 기존 Herdr session의 무결성과 모바일에서의 복구 가능성을 먼저 완성한다.
**추천:** P0-02의 남은 실기기 확인(reconnect, PWA)을 닫은 뒤
P0-03~06을 격리 환경에서 검증한다. 이어서 P1-01/02 Codex adapter로 확장한다 — 이유는
remote 사용 경로와 잘못된 process 입력 방지가 Rich Chat 기능보다 먼저 증명돼야 하기 때문이다.
UI/UX 개편(P1-03, P1-09~P1-11)은 2026-09-25 소유자 요청으로 이 순서보다 먼저 진행했다.
남은 실기기 확인은 P0-02와 같은 실기기 세션에서 한다.
