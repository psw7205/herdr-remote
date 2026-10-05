# 구현 검증 기록

날짜별 section을 추가한다. 각 section은 기록 시점의 관찰이며 현재 동작의 기준이 아니다. 설계 기준은
`docs/prd.md`와 `docs/adr.md`, 현재 구조는 `docs/architecture.md`, Herdr 조사 결과는
`docs/records/integration-findings.md`다.

## 첫 vertical slice 검증 (2026-09-24)

범위: 기존 Herdr Claude 발견 → native transcript를 Chat에 표시 → 모바일 prompt를 동일 PTY/native
session에 전달 → transcript 응답을 Chat에서 확인. 입력은 fork `psw7205/herdr`의 `mobile-binding`
branch(기록 당시 local `codex/mobile-binding`, ADR-036)가 추가한 `agent.binding`·`agent.bound_input`을
거친다. patch는 macOS Claude process의 PID/start time/native metadata를 확인하고, text와 Enter를 PTY에
쓰기 직전 binding을 다시 검증한다.

- `herdr` repo `just ci`: 3,463개 테스트 통과, macOS release binary 빌드 통과.
- 이 repo `go test -race ./...`, `go vet ./...`, `pnpm --dir web test`,
  `pnpm --dir web build` 통과.
- Herdr live handoff 후 두 Claude PID, shell PID, native session ID가 같았다.
  handoff는 새 terminal ID를 발급해 기존 binding을 무효화했다.
- 동일 cwd의 두 Claude pane이 서로 다른 native transcript로 표시됐다.
- 모바일 browser에서 한 prompt를 전송했다. 같은 native transcript에 user/assistant
  record가 각각 한 번 생성됐고 Chat과 PC pane에서 응답을 확인했다. 새 Claude process는 없었다.
- 같은 `command_id` retry가 추가 message를 만들지 않았다. Bridge 및 Herdr에서
  잘못된 binding을 거부했다.
- Browser reload·timeout 뒤 같은 초안이 같은 `command_id`를 재사용하고, 다른
  초안이나 binding은 불확실한 전송이 정리될 때까지 막도록 테스트했다.
- Bridge 중단 중 Claude PID가 유지됐다. 재시작 후 transcript를 복구하고 이전 epoch
  cursor로 WS를 열었을 때 새 snapshot을 보냈다.
- Terminal Esc를 기존 PTY에 전달했다. Chat↔Terminal 전환 전후 pane geometry가 같았다.
- Herdr live handoff 자체는 desktop client 연결을 끊으면서 geometry를 기본 120×40으로
  바꿨다. mobile Terminal은 resize를 호출하지 않는다.

### Tailnet·mobile 경로 (2026-09-24)

실측으로 확인한 것:

- 소유자가 HTTPS Certificates를 켜고 `tailscale serve --bg 8787`을 실행했다. `doctor`의
  `tailnet`은 `https_certificates: true`, `serve_proxy: true`, `funnel: false`, `issues: []`,
  `blockers: []`였고 `tailscale funnel status`는 tailnet only였다.
- `https://<tailnet-host>/api/sessions`가 200과 `herdr.conditional_input: supported`를 반환했다.
- 소유자 phone에서 Tailnet HTTPS로 기존 session과 대화를 열었다. phone에서 보낸 prompt는 같은
  Herdr pane의 기존 Claude Code session에 한 번 도착했고 같은 native session에서 응답했다.
  그 시점 새 command receipt는 `accepted` 하나였고 새 agent process는 없었다.
- phone의 Terminal에 같은 pane 화면이 표시됐다.
- Serve 경유 `Origin: https://evil.example`과 `Origin: null`은 403이었다. 올바른 Origin은
  Origin 검사를 통과했고, 이후 request body 검증의 400으로 dispatch 없이 끝나 receipt 수가
  변하지 않았다.
- client가 넣은 `Tailscale-User-Login` header는 Serve가 실제 identity로 덮어썼고, 소유자
  device의 요청은 수락됐다.

unit test로만 확인한 것: 다른 tailnet 사용자 identity 거부(단일 사용자 tailnet이라 실측 불가).
확인하지 않은 것: 화면 잠금·네트워크 전환 후 WebSocket reconnect, PWA 홈 화면 설치. phone
화면이 full screen이 아니었던 관찰은 원인을 진단하지 않았으며 [backlog](https://github.com/psw7205/herdr-remote/issues)
P1-05/P1-06에 남겼다.

## Binding 손실과 agent 종료 구분 (2026-09-24)

범위는 backlog P0-07이다. fake gateway를 쓴 `internal/session` unit test와 `web/src/api.test.ts`로
확인했다.

| 동작 | test |
| --- | --- |
| binding·capability를 잃어도 같은 pane에서 claude가 보고되면 `unverified`로 남고, 종료가 확인될 때만 `ended` | `TestBindingLossKeepsSessionUnverified`, `TestBindingLossEndsOnlyOnConfirmedEnd` |
| 같은 native ID의 binding이 다시 검증되면 `active`로 복구 | `TestBindingRecovery`, `TestEndedSessionRevivesOnVerifiedBinding` |
| 검증된 관찰이 continuity보다 우선하고, 다른 native ID를 가리키는 binding은 continuity를 끝냄 | `TestVerifiedObservationWinsOverContinuity`, `TestRejectedBindingForAnotherNativeEndsContinuity` |
| `pane:` item이 같은 pane에서 검증된 native session으로 `superseded`+`successor_id` | `TestPaneItemSupersededByVerifiedNativeSession`, `TestPaneItemSupersededBySessionMovedToItsPane` |
| 모호한 pane, 두 pane에서 검증된 같은 native ID, 복제된 pane은 continuity·supersession 없음 | `TestAmbiguousPaneGetsNoContinuity`, `TestSameNativeVerifiedOnTwoPanesIsAmbiguous`, `TestDuplicatedPaneSupersedesNothing` |
| Refresh와 읽기 동시 실행 | `TestConcurrentRefreshAndReads` |

확인하지 않은 것: App의 successor 전환 render test. 알려진 한계: `agent_session`이 없으면 한 Refresh
안의 같은 pane claude→claude 교체를 구분하지 못하고, agent 감지가 순간적으로 빠지면 `ended`가 된다.
supersession은 pane 단위라 같은 pane의 process 교체도 `superseded`로 보인다.

## UI/UX 개편 검증 (2026-09-25)

범위는 backlog P1-03, P1-09~P1-11과 P1-04~P1-06의 일부다. 화면 확인에는 dev 전용 fixture
page(`web/fixture.html`)와 headless Chrome(360·390·412폭, light/dark)을 썼다. fixture는 익명
sample이며, 실제 session으로 찍은 screenshot은 추적 파일 밖에만 두었다.

headless와 fixture로 실측한 것:

- Chat은 긴 대화의 위·중간·아래 어디서나 header가 y=0에 있다. composer 하단은 viewport 하단과
  같고 문서 자체는 scroll되지 않는다. 변경 전에는 맨 아래에서 header가 viewport 위 약 16,600px에
  있었다.
- 메시지 400개 대화를 약 550ms에 그리고 맨 아래에 도착한다. 맨 아래에서는 새 메시지를 따라가고,
  위로 올린 상태에서는 위치가 움직이지 않으며 "새 메시지" 수를 보인다.
- composer 글자 하나의 event 처리 시간이 224–336ms였다. 메시지 목록과 Markdown을 memo로
  분리한 뒤 16ms로 줄었다.
- Back은 Terminal → Chat → 목록 순서로 돌아간다. Terminal deep link에서는 부모 화면으로 교체된다.
- Terminal key bar는 세로·가로 화면 모두 viewport 하단에 붙는다. light theme에서도 Terminal은
  dark token으로 그려진다. xterm 크기 계산과 polling code는 바꾸지 않았다.
- reduced motion에서 작업 중 spinner가 멈춘다.

개발용 Bridge를 별도 port와 별도 receipt directory로 띄워 실제 Herdr session을 read-only로 확인했다.
prompt와 Terminal 입력은 보내지 않았다.

- 목록에 실제 session 3개가 상태 group과 preview로 표시됐다. 같은 project의 session 2개는 제목,
  preview, 시간으로 구분됐다.
- 같은 시점 main code Bridge와 비교하면, 작업 session에서 보이는 user message가 23개에서 2개로
  줄었고 assistant message는 31개로 같았다. 남은 2개는 사용자가 직접 입력한 요청이었다.
- 최종 filter 규칙으로 로컬 transcript 41개를 dry-run했다. invalid는 0건이고, 보이는 message는
  user 220개, assistant 776개였다.

독립 code review의 finding 6건을 고쳤다.

- successor 전환 뒤 이전 history entry로 돌아가도 successor Chat으로 바로 간다.
- Terminal xterm 영역에 가로 notch의 safe-area를 반영했다.
- echo가 숨는 slash command를 보낸 뒤 전달 표시가 8초 안에 사라진다.
- local command 기록은 그 요소만으로 이뤄졌을 때만 숨긴다. marker로 시작하는 실제 기록 94건은
  모두 그런 형태였다.
- Terminal을 쓸 수 없는 Terminal URL은 Chat URL로 바뀐다.
- 중단을 보내기 버튼과 분리했다.

1·3·5·6번은 fixture 흐름에서 다시 확인했다.

자동 검증: `pnpm --dir web test`(152개), `pnpm --dir web build`, `mise run test`, `mise run vet`.

확인하지 않은 것:

- Android 실기기의 keyboard 동작(`interactive-widget=resizes-content`)과 설치형 PWA
- iOS Safari
- 소유자가 지정한 session에 실제 prompt를 보내 한 번만 도착하는지
- 실기기의 code 복사와 text selection

이 항목은 P0-02(화면 잠금 뒤 재연결), P0-06(실제 interrupt)과 같은 실기기 세션에서 함께 확인한다.

## Terminal 크기 맞춤과 PWA full screen 관찰 (2026-09-25)

범위는 backlog P1-05·P1-06이다. 방식은 [architecture §4.4](../architecture.md#44-terminal)에 있다.

- headless Chrome 390×844: 변경 전 terminal 폭 867px에 줄바꿈이 틀렸고, 변경 후 6px에서 128×40
  전체가 올바른 줄바꿈으로 보이며 약 90px의 horizontal scroll이 남는다. landscape는 9px에서 fit되고
  높이 500px에서도 key bar가 보인다.
- 2026-09-24 phone에서 화면이 full screen이 아니었던 관찰은 같은 headless 조건에서 재현되지 않았다.
  layout 높이가 viewport와 같았다. 가장 유력한 원인은 설치된 PWA가 아닌 browser tab 실행이라
  manifest의 `display: standalone`이 적용되지 않은 것이다.

확인하지 않은 것(실제 phone): iOS keyboard와 key bar, pinch, 6px 가독성, IME, Esc/Tab/arrows/Ctrl-C/Enter,
horizontal scroll, 설치된 PWA의 full screen.

## 배색 교체 검증 (2026-09-27)

범위는 backlog P1-11의 배색과 P1-06의 icon이다. Herdr website의 warm neutral 바탕과 blue 채움
버튼을 cool graphite로 바꿨다. 회색은 accent hue(OKLCH 255) 쪽으로 약간 기울이고, 주 버튼은 먹색,
blue는 link·focus·눌린 상태에만 쓴다. 상태 색의 hue는 유지했고 `--unknown`만 같은 기준의 회색으로
바꿨다.

- `web/src/tokens.css`의 token 쌍으로 WCAG 대비를 계산했다. 글자는 light 4.90:1, dark 6.15:1
  이상이고, 상태 표시와 focus는 light 3.11:1, dark 4.71:1 이상이다.
- 계산 중 목록 group 제목 옆의 수가 비활성용 `--faint`(2.6:1)로 그려지는 것을 찾아 `--muted`로
  바꿨다.
- fixture를 390폭 light/dark로 다시 찍었다. 목록, Chat, 보내기, 입력 필요, Terminal에서 blue는
  link와 Terminal "맞춤" 눌린 상태에만 보였다. 보내기 버튼은 light에서 먹색 바탕이고, Terminal은
  light theme에서도 dark canvas와 같은 바탕으로 그려진다.
- production build에서는 Terminal의 dark 고정이 풀려 light theme의 header·key bar가 밝게 그려졌다.
  build의 lightningcss가 `light-dark()`를 token을 선언한 `:root`에서 해석되는 변수로 낮추기
  때문이다. dev server는 native `light-dark()`를 써서 2026-09-25 fixture 확인에서는 드러나지 않았다.
  color token을 `[data-scheme]` 요소에도 선언하도록 고쳤다. fixture를 같은 `vite.config.ts`로
  build해 보면 light theme Terminal header 바탕이 고치기 전 `rgb(241, 244, 247)`, 고친 뒤
  `rgb(17, 20, 24)`다.
- icon, manifest 색, light/dark `theme-color`를 새 값으로 바꾸고 service worker cache를
  `herdr-chat-shell-v3`로 올렸다. PNG icon은 SVG에서 `rsvg-convert`로 다시 만들었다. 이전
  `icon-192.png`는 headless Chrome으로 만들면서 icon 일부만 잘려 있었다.

자동 검증: `pnpm --dir web test`(152개), `pnpm --dir web build`.

확인하지 않은 것: 이미 설치한 PWA의 icon·`theme-color` 갱신, 실기기 화면의 색.

## 공개 전 보안 수정 검증 (2026-09-28)

범위는 backlog P0-09의 공개 전 review에서 찾아 고친 결함이다. 아래 항목은 모두 unit test로
확인했다. 실제 host의 Tailscale Serve나 설치된 PWA에서 다시 측정하지는 않았다.

| 수정 | 경계 | unit test |
| --- | --- | --- |
| Serve는 client `Host`를 그대로 넘긴다. tailnet host가 확정되지 않은 동안 loopback `Host`를 위조한 요청이 통과하던 우회를 막았다. Serve가 붙이는 `X-Forwarded-*`·`Tailscale-User-*` header가 하나라도 있으면 원격 요청으로 보고, 진짜 Serve 요청은 tailnet host와 소유자 login을 모두 요구한다(ADR-024). | `internal/httpapi` | `TestForgedLoopbackHostWithServeHeadersIsRejected`, `TestServeRequestRequiresTailnetHostAndOwner` |
| 모든 응답(거부 응답 포함)에 `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`을 싣고, static directory는 listing 대신 404를 반환한다. | `internal/httpapi` | `TestResponsesCarrySecurityHeadersAndStaticHasNoListing` |
| `ended`·`superseded` item의 transcript watcher를 닫아, 본 적 있는 session 수에 비례하던 fd 증가를 없앴다. 취소된 watcher는 읽던 중이어도 item에 publish하지 않는다. | `internal/session` | `TestTranscriptWatcherFollowsItemLifecycle`, `TestCancelledWatcherPublishesNothing` |
| Bridge listen 주소로 향하는 Serve raw TCP forward(background·foreground 설정)를 Funnel처럼 `doctor`의 blocker로 보고한다. 다른 port로 향하는 forward는 보고하지 않는다. | `internal/tailnet`, `cmd/doctor` | `TestServeConfigTCPForward`, `TestDoctorBlocksTCPForwardToBridge`, `TestDoctorIgnoresTCPForwardToOtherPort` |
| service worker cache 이름을 build 산출물의 hash(build id)로 자동 갱신한다. placeholder가 없으면 build가 실패한다. | `web/plugins/swBuildId.ts` | `web/plugins/swBuildId.test.ts`의 `service worker build id` 4개 |

자동 검증(2026-09-28): `mise run test`(`go test -race ./...`)에서 test가 있는 9개 package가
모두 통과했다. `mise exec -- pnpm --dir web test`는 14개 file, 156개 test가 통과했다. 위 표의
test는 `-run`으로 따로 실행해도 통과했다.

같은 날 Apache-2.0 LICENSE 추가, Go module 경로 변경, `git filter-repo`로 history의 host 보안 상태
서술 치환, fork `master`의 upstream sync와 `mobile-binding` 공개를 마친 뒤 repo를 공개했다(P0-09).

확인하지 않은 것: 실제 Serve 경유로 위조 `Host` 요청이 거부되는지, 실제 browser에서 frame 삽입이
막히는지, 장기 실행 Bridge의 실제 fd 수, 실제 `tailscale serve --tcp` 설정에서의 `doctor` 출력,
이미 설치한 PWA의 cache 갱신. 남은 resource 한계는 backlog P1-13에서 추적한다.

## 격리 Herdr session integrity 검증 (2026-09-28)

범위는 backlog P0-04·P0-05·P0-06의 잘못된 process 입력, 조용한 중단, stock 전환이다.
실행 방법은 [CONTRIBUTING.md](../../CONTRIBUTING.md#격리-herdr-환경)의 격리 환경 절차로 정리했다.

환경: `env -i`로 띄운 이름 있는 headless Herdr server(patched `0.9.1`), 임시 Git directory의
workspace, Claude Code `2.1.283`(Haiku 4.5), 이 repo의 Bridge를 별도 port·receipt directory와
`-tailnet-host off`로 실행. desktop client는 붙이지 않았다. 기존 default Herdr server에는 연결하지
않았고, 실행 전후 default의 pane 9개(`pane_id`·`terminal_id`·agent)와 기존 process PID가 같았다.

| 시나리오 | 결과 |
| --- | --- |
| 기본 전달 | Bridge prompt가 transcript에 user/assistant 한 번씩 기록됐다. 같은 `command_id` retry는 202이고 message가 늘지 않았다. 같은 ID에 다른 body는 409 `COMMAND_ID_CONFLICT` |
| A 종료 → 같은 pane의 shell | A binding의 `prompt`·`terminal_input`·`interrupt`가 모두 409 `SESSION_CHANGED`. shell에 입력이 나타나지 않았다. 종료된 item은 목록에서 빠지고, claude pane이 없으면 `conditional_input`은 `unknown`이다(설계대로) |
| A → 같은 pane의 새 session B | B는 새 native ID와 새 binding을 받았다. A binding으로 A·B ID에 보낸 세 종류 command가 모두 409 `SESSION_CHANGED`. B transcript에는 B 자신의 대화만 있다 |
| 같은 process의 `/clear` | 새 native ID에 새 binding이 발급됐다. 이전 binding은 이전·새 ID 모두에서 409 |
| `/exit`과 Bridge prompt 경합 | native session이 있는 상태에서 `/exit` 입력과 prompt 사이 간격을 0~400ms로 바꿔 7회 실행. 6회는 `accepted`, 400ms는 409 `SESSION_ENDED`. shell 실행을 드러내는 marker 파일은 한 번도 생기지 않았다. `accepted` 6회 중 5회는 종료 중인 같은 Claude에 쓰였다가 transcript에 남지 않았고, 1회는 같은 Claude가 prompt를 처리했다 |
| interrupt | Bash tool `sleep 40` 실행 중 Bridge `interrupt`가 202, 약 2초 뒤 `idle`, pane에 `Interrupted`. binding 유지 |
| WS 종료 | `sleep 12` 작업 시작 6초 뒤 events WS를 닫았다. agent는 작업을 끝내고 최종 message를 기록했다. binding 유지 |
| permission 대기 | default permission mode에서 Bash 권한 요청 시 status `needs_attention`. Chat prompt는 409로 거부(이 검증에서 code를 `SESSION_CHANGED`에서 `AGENT_NOT_READY`로 고쳤다). binding을 붙인 Terminal 조회는 200, `terminal_input` Esc로 거절하자 파일이 만들어지지 않고 `idle`로 돌아왔다 |
| patch → stock live handoff | `herdr server live-handoff --import-exe <stock 백업>`. Claude PID·shell PID 유지. `/api/sessions`는 `conditional_input: unsupported`, item은 `unverified`·`terminal: false`·binding 없음. 이전 binding의 세 종류 command는 Bridge에서 409 `SESSION_CHANGED`(binding이 비어 Herdr까지 가지 않는다). `doctor`는 `unsupported`와 blocker를 보고했다 |
| stock → patch live handoff | PID 유지, `supported`, item `active`, 새 binding. 이전 binding은 409, 새 binding prompt는 전달되고 응답했다 |

부수 관찰:

- 처음 여는 폴더에서 `agent start`는 신뢰 확인 화면이 뜬 채 `agent_not_ready`("blocked during
  startup")로 끝났다. 그 시점의 binding 발급 여부는 측정하지 않았다(P1-14).
- `agent start`는 pane의 사용자 shell에서 agent를 실행하므로 shell alias·함수가 적용된다.
  새 session의 permission mode는 사용자 shell 환경을 따르며 Bridge가 보장하지 않는다.
- coding agent 안에서 Herdr server를 띄우면 그 agent의 환경변수를 물려받는다. Claude Code는
  상속된 child-session 표식 때문에 transcript를 저장하지 않았다. `env -i`로 다시 띄워 해결했다.

알려진 잔여 창: Herdr는 text와 Enter를 쓰기 직전에 binding을 다시 검증한다. 검증을 통과해 쓴
byte를 종료 직전의 process가 읽지 않고 끝나면, 다음 foreground process(shell)가 읽을 수 있다.
위 경합 7회에서는 관찰되지 않았고 guard가 막는 범위도 아니다.

확인하지 않은 것: stale binding으로 Terminal 조회가 거부되는지(A→shell 단계의 요청에
binding header가 빠져 근거로 쓰지 않는다), handoff 전후 pane geometry(P1-08, desktop client
없음), patch → patch 재적용, 새 release rebase(upstream 최신이 `v0.9.1`), Bridge 재시작.
실행 후 격리 server·Bridge·test transcript는 삭제했다. Claude Code의 폴더 신뢰 기록은 사용자
설정이라 지우지 않았다.

## Transcript 이동·resource 상한 검증 (2026-09-29)

범위는 backlog P1-12·P1-13이다. 격리 환경은 [CONTRIBUTING.md](../../CONTRIBUTING.md#격리-herdr-환경)
절차(patched Herdr `0.9.1`, Claude Code `2.1.284`, Haiku 4.5)를 따랐고, 실행 전후 default server의
pane 7개와 기존 Claude PID가 같았다.

| 항목 | 방법 | 결과 |
| --- | --- | --- |
| worktree 이동 (P1-12) | 격리 Claude에 `EnterWorktree` tool 사용을 요청 | Claude Code가 transcript를 `<project>--claude-worktrees-<name>` project directory로 옮기고 원래 파일을 지웠다. Bridge는 다음 `Refresh`에서 `transcript moved`를 남기고 새 epoch snapshot으로 바꿨다. 이동 전 2개와 이동 응답까지 message 4개가 중복 없이 보였다 |
| 이동 뒤 전달 | 새 경로에서 Bridge `prompt` | `accepted`, 같은 epoch에 user/assistant가 live로 추가됐다 |
| transcript fd (P1-13) | default server에 개발용 Bridge를 read-only로 붙여 `lsof`. 입력은 보내지 않았다 | Chat session 2개일 때 변경 전 build(`de0f987`)는 fd 82개, 그중 project directory의 transcript 42개를 열었다. 변경 후 build는 fd 19개, transcript 2개다 |

Unit test 근거:

| 동작 | test |
| --- | --- |
| verified·unverified item이 이동한 transcript를 native ID로 다시 찾아 새 epoch로 복구하고, 두 곳에서 보이면 기존 watcher를 유지 | `internal/session` `TestMovedTranscriptIsResolvedAgain` |
| transcript 파일만 감시해 같은 directory의 다른 파일을 열지 않음 | `internal/transcript` `TestWatchDoesNotOpenSiblingTranscripts`(변경 전 구현에서 fd 69개로 실패) |
| 삭제 후 재생성, rename 교체 뒤에도 처음부터 다시 읽고 이후 append를 받음 | `TestWatchFollowsRecreatedAndReplacedFile` |
| 종료된 item은 stream snapshot을 유지하되 parsed history를 해제하고, 최근 종료된 32개만 남김 | `internal/session` `TestEndedItemsReleaseHistoryAndAreBounded` |
| MagicDNS host가 없어도 Funnel과 Tailscale Service의 raw TCP forward를 blocker로 보고 | `cmd/doctor` `TestDoctorBlocksExposureWithoutMagicDNS`, `internal/tailnet` `TestServeConfigTCPForward` |

확인하지 않은 것: `fsnotify.NewWatcher` 생성 실패 시 polling으로 대체하고 재시도하는 경로는
test로 재현하지 않았다. 수 주 단위 장기 실행의 fd·memory 추이도 측정하지 않았다. 실행 후 격리
server, Bridge, test transcript directory 2개를 삭제했다.

## 격리 Herdr 새 session 시작 검증 (2026-09-29)

범위는 backlog P1-14(ADR-037)다. [격리 환경 절차](../../CONTRIBUTING.md#격리-herdr-환경)를
session 이름 `e2e-p114`, Bridge port 8798, `-project-root <임시 root>`로 실행했다.

환경: `env -i`로 띄운 headless patched Herdr `0.9.1`, Claude Code `2.1.284`, desktop client 없음.
임시 root 아래에 Git repo 두 개, hidden Git repo, Git이 아닌 directory, root 밖 Git repo를 가리키는
symlink를 두었다. default Herdr server에는 조회(`pane list`·`agent list`·`pane.process_info`)만
보냈다. 실행 전 default의 pane 7개는 끝난 뒤에도 같은 `terminal_id`·agent였다. 실행 중 default에
pane 하나가 늘고 다른 한 pane의 Claude가 같은 shell에서 새 process로 바뀌었다. 이 검증의 명령은
default에 쓰지 않았으므로 원인은 확인하지 못했다. 검증을 실행한 coding agent의 process는 그대로였다.

설치 version에서 확인한 API 사실:

- raw socket `agent.start`는 입력을 보낸 직후 `launch_pending: true`로 반환한다. 30s 준비 대기와
  `agent_not_ready`는 Herdr CLI가 `agent.get`을 polling해 만든다. Bridge도 같은 규칙(`blocked` →
  미준비, `idle`/`done` + `interactive_ready` → 준비, 새 pane의 shell 초기화 중 `agent_pane_busy`만
  2s 동안 재시도)으로 기다린다.
- `workspace.list`의 workspace에는 cwd가 없다. workspace의 폴더는 `session.snapshot`의 첫 pane `cwd`로
  읽는다. pane cwd는 symlink를 해석한 실제 경로로 보고되므로 후보 경로와 양쪽 모두 해석해 비교한다.
- `workspace.create`·`tab.create`에 `focus: false`를 주면 기존 focused workspace가 바뀌지 않았다.

| 시나리오 | 결과 |
| --- | --- |
| 후보 목록 | root의 Git repo 두 개만 나왔다. hidden repo, Git이 아닌 directory, root 밖 symlink는 빠졌다. 이미 열린 workspace의 폴더는 `open`·`new_tab`으로 표시됐다 |
| (a) 새 workspace, 처음 여는 폴더 | 약 5.5초 뒤 202 `accepted`·`AGENT_NOT_READY`와 생성된 workspace·tab·pane ID. 신뢰 확인 화면 동안 목록 item은 `pane:<pane-id>`·`unbound`·`needs_attention`·`terminal: false`로 binding이 없어 모바일 Terminal은 열리지 않았다. `pane send-keys <pane> down enter`로 수락하자 `active`·`terminal: true`가 됐고, 첫 prompt 뒤 기존 discovery로 `claude:<native-id>`·`chat: true`가 됐다 |
| (b) 열린 workspace의 새 tab | 신뢰한 폴더에서 약 6.6초 뒤 202 `accepted`(code 없음). 같은 workspace에 tab이 하나 늘고 workspace 수는 그대로였다 |
| (c) 같은 `command_id` retry | (a)·(b) 모두 같은 status·code·생성 ID를 반환했고 workspace·tab 수가 늘지 않았다. Bridge를 재시작한 뒤에도 같은 결과였고 후보 ID도 재시작 전후 같았다 |
| (d) 목록 뒤 폴더 변경 | 목록을 받은 뒤 한 repo directory를 지우고 다른 하나를 root 밖 repo로 가는 symlink로 바꿨다. 두 요청 모두 409 `CANDIDATE_CHANGED`, workspace가 생기지 않았다 |

부수 관찰:

- 시작된 Claude의 terminal title에 사용자 shell alias가 붙인 permission 관련 옵션이 보였다. 새 session의
  permission mode는 사용자 shell 환경을 따르며 Bridge가 보장하지 않는다(UI에 표시).
- Bridge로 시작한 session에서 `/model`을 쓰면 사용자 설정의 기본 model이 바뀐다. 이 검증 중 바뀐 값은
  원래대로 되돌렸고 절차에 주의를 추가했다.
- 정상 시작도 5~7초 걸렸다. Bridge는 start 한 건을 50s로, client는 60s로 제한한다.

확인하지 않은 것: 실제 `delivery_unknown`과 `AGENT_START_FAILED` 발생(unit test로만 확인), Tailnet
경유 실기기 화면, 신뢰 확인 화면의 수락을 모바일에서 하는 경로(binding이 없어 현재 불가), desktop
client가 없을 때 만든 pane의 크기(P1-08). Web UI는 unit test와 build로 확인했고 browser 화면은
사용하면서 확인한다. 정리: Bridge 종료 → 격리 server 종료 → session 삭제 → 임시 directory 이름의
test transcript 삭제. Claude Code의 폴더 신뢰 기록은 사용자 설정이라 지우지 않았다.

## Herdr 0.9.3 patch 전환 검증 (2026-09-30)

범위는 `mobile-binding` patch의 `v0.9.1` → `v0.9.3` rebase, stock `0.9.3`에서의 조건부 입력
판정, 실제 server 적용이다. 절차는 [Herdr patch runbook](../herdr-patch.md#7-upgrade-정책)을 따랐다.

시작 상태: `herdr update`로 설치 binary와 실행 중 server가 stock `0.9.3`으로 바뀌어 있었다. 배포된
Bridge는 `/api/sessions`에 `conditional_input: supported`를 보고했고 item은 `pane:`·`unbound`였다.
Herdr `0.9.2`부터 unknown method 거절 응답이 request ID를 그대로 돌려주는데(upstream #4344), Bridge는
ID가 빈 응답만 미지원으로 판정해 이 거절을 method-level API error로 해석했다. binding 검증은 실패했으므로
입력은 전달되지 않았고 capability 표시만 틀렸다. 판정을 고친 뒤 실제 stock server에서 `doctor`가
`unsupported`와 blocker를 보고했다.

환경: `env -i`로 띄운 이름 있는 격리 server(설치된 stock `0.9.3`으로 시작), 임시 Git directory,
Claude Code(Haiku 4.5), 수정한 Bridge를 별도 port·receipt directory와 `-tailnet-host off`로 실행.

| 시나리오 | 결과 |
| --- | --- |
| stock `0.9.3` | `doctor`와 Bridge 모두 `unsupported`, `verified_binding: false` |
| stock → patch live handoff | Claude PID 유지, terminal ID 새로 발급. `doctor`는 `supported`·`verified_binding: true`·blocker 없음 |
| 발급하지 않은 token의 `agent.bound_input` | `runtime_binding_mismatch`. pane에 marker text가 나타나지 않았다 |
| 새 token의 `agent.bound_input` prompt | `ok`, Claude가 응답했다 |
| Bridge 목록 | `supported`, item `claude:<native-id>`·`active`·`terminal: true` |
| Bridge prompt, 같은 `command_id` 두 번 | 두 번 모두 202 `accepted`, pane에 prompt가 한 번만 들어갔고 응답도 한 번이었다 |
| patch → stock → patch handoff | stock에서 `unsupported`·item `unverified`·`terminal: false`, patch로 돌아오면 `supported`·`active`. Claude PID 유지 |

실제 server 적용: stock `0.9.3`을 `herdr-remote-original-0.9.3`으로 백업하고 patch를 설치한 뒤
`herdr server live-handoff --import-exe <install-dir>/herdr`로 적용했다. pane 8개와 Claude PID가
유지됐고 terminal ID가 새로 발급됐다. `doctor`는 `supported`·`verified_binding: true`, Bridge를
redeploy한 뒤 `/api/sessions`는 `supported`와 `claude:` item `active`를 보고했다.

Herdr test: `cargo nextest` 3,694개 중 5개(`api_ping`·`multi_client` 각 2개, `live_handoff` 2개)가
실패했고, stock `v0.9.3`에서도 같은 5개가 실패했다. 원인은 unresolved다. patch 회귀 여부는 격리 handoff
실측으로 판단했다.

확인하지 않은 것: patch → patch 재적용, handoff 전후 pane geometry(P1-08), Tailnet 경유 실기기 화면,
Bridge interrupt·Terminal 입력(이번에는 prompt만 보냈다). 정리: Bridge 종료 → 격리 server 종료 →
session 삭제 → test transcript와 임시 directory 삭제. default server의 pane은 격리 검증 전후 같았다.

## Claude transcript live 기록 단위 측정 (2026-10-05)

범위는 [P1-07](https://github.com/psw7205/herdr-remote/issues/9)의 Claude 쪽 delta 측정이다. 결과와 해석은
[integration findings](integration-findings.md#2026-10-05--claude-transcript-live-기록-단위와-tool-식별자)에 있다.

환경: [격리 Herdr 환경](../../CONTRIBUTING.md#격리-herdr-환경)을 session 이름 `e2e`로 실행했다. `env -i`로 띄운
Herdr `0.9.3`, Claude Code `2.1.289`(`--model claude-haiku-4-5-20251001`), 파일 세 개를 둔 임시 directory.
이 실행 환경의 정책 때문에 임시 directory에서 `git init`은 하지 못했다. 측정은 transcript 파일만 보므로
Bridge는 띄우지 않았다. 신뢰 확인 화면은 `pane send-keys <pane-id> down enter`로 수락했고 Claude가 바로
준비돼 `agent start`를 다시 하지 않았다. default server에는 `pane list` 조회만 보냈다.

| 시나리오 | 실측 |
| --- | --- |
| tool 없는 긴 답변 | 완성 전 assistant record 0개, 끝의 미완성 줄 0번, 기존 부분 변경 0번. thinking·text record가 같은 sample에 한꺼번에 나타남 |
| Bash 한 번 | `tool_use.id` = `tool_result.tool_use_id`, 두 record 사이 약 1.6초, 뒤의 text는 새 `message.id` |
| 한 응답 안의 Bash 병렬 두 번 | 같은 `message.id`의 두 `tool_use` 사이에 첫 `tool_result`가 끼고, parent chain은 한 줄로 이어짐 |

unit test: `internal/claude`의 `TestProjectionOfPerBlockRecords`가 위 모양의 익명 fixture(thinking·text·
tool_use가 따로 쓰인 message, 병렬 tool_result, attachment·system record)로 Chat message가 text record만
순서대로 남는지 확인한다. Web의 작업 중 표시는 기존 test(`api.test.ts`, `presentation.test.ts`,
`SessionChat.test.tsx`)로 확인했고 바꾸지 않았다.

확인하지 않은 것: Bridge를 거친 실제 Chat 화면과 event 순서, Haiku 외 model의 기록 시점, Codex.
정리: 격리 server 종료 → `herdr session delete e2e` → 임시 directory 이름의 test transcript directory 삭제 →
임시 directory 삭제. default server의 pane·`terminal_id`·agent와 Claude PID는 실행 전과 같았다.
Claude Code의 폴더 신뢰 기록은 사용자 설정이라 지우지 않았다.

## Changed Files·diff 검증 (2026-10-05)

범위는 [P2-03](https://github.com/psw7205/herdr-remote/issues/16)이다. 구현 경계는
[ADR-016](../adr.md#adr-016--changed-files는-git을-read-only-source로-사용한다)의 2026-10-05 note에 있다.

unit test(`internal/gitstate`, `internal/httpapi`, 임시 Git repo):

| 항목 | 결과 |
| --- | --- |
| modified·untracked added·deleted·renamed·binary·commit 없는 repo·Git이 아닌 directory | 목록과 줄 수, diff 형식 확인 |
| repo config의 fsmonitor·hook·external diff·diff driver·textconv·clean/smudge filter | plain git에서는 여섯 경로가 모두 실행되고(양성 대조) Reader에서는 하나도 실행되지 않음. `.git/index`의 내용과 mtime 불변 |
| git 2.31 미만 | 시작 시 거부, `GIT_UNAVAILABLE` |
| 목록에 없는 path, `./`·`../`·절대 경로·NUL·`:(glob)` | 거부. glob 문자가 든 file 이름은 확장되지 않음 |
| untracked symlink·FIFO | 읽지 않음(`content: none`) |
| 목록 500개·diff 256 KiB 초과 | 잘라서 `truncated` 표시 |
| 허용 목록 밖 Origin, 없는 session, 종료된 item | 각각 403, 404, 읽기 허용 |

Web: route·parsing·화면 test와 fixture page(`changes`, `changes-large`, Git이 아닌 directory)를 headless
Chrome으로 확인했다. 확인하지 않은 것: 실제 session project에서 Bridge를 띄운 화면, production build의
dark mode, 실기기 화면, submodule이 있는 repository.

## Tool activity card 검증 (2026-10-05)

범위는 [P2-01](https://github.com/psw7205/herdr-remote/issues/14)의 Claude tool card다. protocol은
[ADR-039](../adr.md#adr-039--tool-호출은-chat-item으로-투영하고-결과는-message-갱신으로-보낸다)에 있다.

unit test(`internal/claude`, `internal/session`, `web/src`, 익명 synthetic fixture): 한 tool의 running →
completed가 epoch를 바꾸지 않는 `message.updated`로 나가는 것, 병렬 tool의 교차 순서, `is_error`, 짝 없는
result 무시, result 없이 다음 turn으로 넘어간 호출의 `unknown`, 해석할 수 없는 tool block이 transcript를
invalid로 만들지 않는 것, 8 KiB 상한, 재접속 replay와 snapshot의 최종 상태, client의 갱신·묶음·상세 보기를 확인했다.

로컬 transcript 74개를 read-only로 투영해 개수만 셌다. invalid 0, tool item 3,191개(completed 3,092,
error 76, unknown 23, running 0), 상한에 걸린 result 200건·input 23건이었다. fixture page(`tools`)는
headless Chrome 390폭에서 light·dark로 확인했고 page 가로 넘침은 없었다.

확인하지 않은 것: 실제 Herdr session에서 Bridge를 거친 running → completed 갱신, 실기기 화면, tool이 많은
긴 session의 snapshot 크기와 memory 사용량.

## 실행 중인 version 확인 검증 (2026-10-05)

범위: Bridge가 `go build`의 VCS stamp와 `web/dist/index.html`의 client build id를 `GET /api/sessions`의 `bridge`와
시작 로그에 보고하고, 세션 목록이 short revision과 새로고침 안내를 보이며, `scripts/deploy.sh status`·`redeploy`가
실행 중인 Bridge를 HEAD·`web/dist`와 대조하는 것(commit `45a7c1a`). release tag는 쓰지 않는다.

### Unit test만으로 확인

- Go: vcs setting이 없는 `go run` build·clean·dirty의 stamp 해석, `index.html` meta 읽기(파일 없음, placeholder,
  16자리 소문자 hex만 인정), `/api/sessions`의 `bridge` 필드, dist를 다시 build하면 재시작 없이 다음 응답에서
  바뀌는 `client_build`.
- Web: `sw.js`와 `index.html`에 같은 id stamp, placeholder가 없는 파일 이름을 가리키는 오류, 로드된 build id
  해석(placeholder·meta 없음은 unknown), label(`65f5c6b`, `65f5c6b-dirty`, `dev`), 두 id가 모두 있고 다를 때만
  stale, `listSessions`의 `bridge` 매핑(없으면 `null`), 세션 목록 subtitle과 새로고침 안내 조건.
- 전체: `go vet`, `go test -race ./...`(cold 7.5s), `pnpm test` 218개, `pnpm build` 뒤 `index.html` meta와 `sw.js`
  cache 이름이 같은 id, `shellcheck scripts/deploy.sh`.

### 실측

- 임시 module에서 `go run`은 VCS stamp를 심지 않고 `go build`는 심으며, untracked 변경만 있어도 `vcs.modified=true`인
  것을 확인했다.
- fixture `stale-client`와 `default`를 headless Chrome(390×844)으로 캡처해 subtitle `7개 실행 중 · 3c7a1e9`와
  새로고침 Notice의 표시·미표시를 확인했다. 캡처는 추적 파일에 넣지 않았다.
- scratch `PREFIX`와 별도 LaunchAgent(port 8797, `-tailnet-host off`, 별도 receipt directory): `install`이 설치
  revision을 출력했고, 시작 전 `status`는 `running none`으로 1, `redeploy` 뒤 `status`는 일치로 0이었다. feature
  commit 뒤 같은 Bridge에 `status`를 다시 실행하자 `running Bridge is 4f4ee9a, HEAD is 45a7c1a`로 1을 반환했다.
  정리: bootout, scratch 삭제, 남은 process 없음.
- 실제 LaunchAgent: 첫 `redeploy`는 설치까지 끝났지만 health 10s 안에 응답이 없어 `.prev`로 되돌리고 다시
  시작했다. 같은 시각에 LaunchAgent plist가 수정·재등록되어(다른 세션의 작업, `-project-root` 추가) kickstart와
  겹친 것으로 보이며, 새 binary의 로그 줄은 없었다. 새 binary를 같은 flag(명시 tailnet host·login, `-project-root`
  2개)로 다른 port에서 직접 띄우자 2.5s 안에 200과 `bridge` 필드를 반환했다. 두 번째 `redeploy`는 성공했다:
  `running Bridge is 45a7c1a`, `status`는 HEAD·`web/dist`와 일치, launchd `last exit code = 0`, `.prev`에 이전
  binary(9/30 build) 보존, 시작 로그에 `revision`·`modified=false`·`client_build`.
- `redeploy` 전체는 test cache가 유효할 때 약 5s, cold `go test -race`는 7.5s였다.

확인하지 않은 것: 실기기에서 subtitle과 새로고침 안내 표시, service worker가 이전 shell을 캐시한 상태에서 새 build
뒤 실제 안내 노출, `redeploy`가 `.prev`로 되돌린 직후 `status`가 불일치를 보고하는 경로(이번에는 restore 직후 Bridge가
아직 뜨지 않아 `running none`이었다), 설치된 plist의 `RunAtLoad=false` 때문에 재부팅 뒤 Bridge가 자동 시작되지 않는
문제(code 범위 밖, 사용자 설정).

### LaunchAgent `stop`·`start`·`restart` task 실측 (2026-10-05)

- scratch LaunchAgent(port 8797, `RunAtLoad=false`, plist는 `~/Library/LaunchAgents` 밖): plist를 찾지 못하면
  `start`가 찾은 경로를 적은 오류로 1, 내려간 상태의 `stop`은 안내만 하고 0, `HERDR_REMOTE_LAUNCHD_PLIST`로
  `start`하면 bootstrap·kickstart 뒤 health 응답, `restart`는 올라간 job의 `path =`로 plist를 찾아
  bootout→bootstrap→kickstart(인자 17줄 유지), `stop` 뒤 port listener 0, 수동 bootstrap만 한 상태에서 `start`는
  "already loaded" 안내 뒤 kickstart로 기동. 정리 뒤 job·process 없음.
- 실제 LaunchAgent: `restart`→`stop`→`start`→`status` 순서로 실행해 각각 정지·기동과 health 응답을 확인했고 Bridge
  로그에 두 번의 재기동이 남았다. `status`는 docs commit으로 HEAD가 움직인 상태라 `running Bridge is 45a7c1a, HEAD is
  9adedc1`로 1을 반환했다. revision 비교는 그 commit이 code를 바꿨는지 보지 않는다.
- `shellcheck scripts/deploy.sh`, `git diff --check`.

확인하지 않은 것: plist `ProgramArguments`를 실제로 바꾼 뒤 `restart`로 바뀐 인자가 반영되는지(scratch에서는 인자 수만
확인), `bootout`이 10s 안에 끝나지 않는 경우.
