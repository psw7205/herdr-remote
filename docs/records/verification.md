# 첫 vertical slice 구현 및 검증 기록

2026-09-24 기준. 설계 기준은 `docs/prd.md`와 `docs/adr.md`, 실제 Herdr 조사 결과는
`docs/records/integration-findings.md`다. 최초 실행 계획은 이전 commit에 남아 있다.

## 목표

기존 Herdr Claude 발견 → native transcript를 Chat에 표시 → 모바일 prompt를 동일 PTY/native
session에 전달 → transcript 응답을 Chat에서 확인한다. Bridge는 agent를 생성하지 않는다.

## 구현된 경계

| 책임 | 파일 |
| --- | --- |
| Herdr public socket 조회와 binding/조건부 입력 | `internal/herdr/gateway.go` |
| Session discovery 및 lifecycle reconciliation | `internal/session/registry.go` |
| Claude native session ID로 transcript resolution | `internal/claude/resolve.go` |
| Claude JSONL의 현재 branch/message projection | `internal/claude/transcript.go`, `internal/claude/projection.go` |
| fsnotify + offset/partial JSONL watcher | `internal/transcript/tailer.go`, `internal/transcript/watch.go` |
| epoch/sequence, bounded replay, atomic subscribe | `internal/stream/session.go` |
| durable command receipt와 retry 차단 | `internal/command/receipts.go` |
| localhost HTTP/WS, Origin/Host/Tailscale owner 검증 | `internal/httpapi/server.go`, `cmd/bridge/main.go` |
| Sessions/Chat/Terminal/PWA | `web/src/`, `web/public/` |
| Herdr native process binding과 PTY queue 검증 | fork `psw7205/herdr`의 `mobile-binding` branch(기록 당시 local `codex/mobile-binding`, ADR-036) |

Herdr patch는 stock `0.9.1`에 없는 `agent.binding` 및 `agent.bound_input`을 추가한다.
macOS Claude process의 PID/start time/native metadata를 확인한다. text와 Enter를 PTY에
쓰기 직전 binding을 다시 검증한다. mismatch는 입력을 취소한다. partial delivery는
`delivery_unknown`으로 간주하며 자동 재전송하지 않는다. Bridge의 command receipt는
conversation 본문을 저장하지 않는다.

## 검증 결과

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
화면이 full screen이 아니었던 관찰은 원인을 진단하지 않았으며 [backlog](../backlog.md)
P1-05/P1-06에 남겼다.

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

확인하지 않은 것: 실제 Serve 경유로 위조 `Host` 요청이 거부되는지, 실제 browser에서 frame 삽입이
막히는지, 장기 실행 Bridge의 실제 fd 수, 실제 `tailscale serve --tcp` 설정에서의 `doctor` 출력,
이미 설치한 PWA의 cache 갱신. 남은 resource 한계는 backlog P1-13에서 추적한다.

## 남은 범위

첫 vertical slice 이후의 Codex adapter는 Herdr 안에서 실행 중인 Codex의 실제 CLI transcript와
native session association을 얻어 별도로 검증해야 한다. Desktop Codex JSONL sample을 CLI
schema로 추측해 재사용하지 않는다. PRD의 Changed Files, attachments, notifications,
structured permission/question은 optional 후속 기능이다.
격리 Codex CLI 테스트에서는 Herdr hook이 native ID를 보고하고 실제 rollout도 확인했다.
Codex `user` record에는 시작 지침도 포함되므로 `content_item_kinds: user.text`만
human prompt로 취급해야 한다. 현재 남은 gate는 보고된 native ID를 foreground process
incarnation에 결합하고 세션 교체 중 조건부 PTY input을 검증하는 것이다. 이 gate가
끝나기 전에는 Codex adapter를 write-enabled로 표시하지 않는다.

Tailnet 배포에서는 localhost Bridge를 Tailscale Serve에 연결하고, `Tailscale-User-Login`
소유자 검증과 exact browser Origin을 모두 적용한다. network ACL/grant의 최소 권한 구성은
[P0-03](../backlog.md#p0--배포와-핵심-안정성)에서 다루며, 전역 tailnet policy 수정은 이
vertical slice의 변경 범위가 아니다. Serve는 2026-09-24에 Funnel 없이 활성화했다.
Bridge 재시작 복구는 사용자 `launchd` job으로 실행하던 때 확인했다. 이 job은 이후 상시 실행이
아니라 필요할 때만 시작하도록 바꿨으며, 실행 방식은 Git 밖의 host 설정이다.

## Backlog 근거 상세

`docs/backlog.md`의 "현재 근거·의존 관계" 셀에서 옮긴 상세 근거다. backlog 셀에는 상태, 의존 관계, 요약, 남은 것만 두고 이 section으로 link한다. 각 문장의 날짜가 기록 시점이다.

### P0-02 실제 mobile Tailnet handoff

`todo`, P0-01 이후.

2026-09-24 소유자 phone에서 Tailnet HTTPS로 확인한 것: 기존 session 목록과 대화 표시. phone에서 보낸 prompt가 같은 Herdr pane의 기존 Claude Code session에 정확히 한 번 도착했고 그 agent가 같은 native session에서 응답했다(그 시점 새 command receipt는 `accepted` 하나, 새 agent process 없음). Terminal에서 pane 화면 표시. Serve 경유 `Origin: https://evil.example`·`Origin: null`은 403이고, 올바른 Origin은 Origin 검사를 통과했다(이후 request body 검증에서 400, dispatch 없음, receipt 수 불변). client가 넣은 `Tailscale-User-Login` header는 Serve가 실제 identity로 덮어쓰며 소유자 device 요청은 수락된다.

남은 것: 다른 tailnet 사용자 identity 거부는 단일 사용자 tailnet이라 unit test로만 검증했다. 화면 잠금·네트워크 전환 후 WebSocket reconnect는 미검증이다. PWA 홈 화면 설치는 P1-06에 남아 있다. phone 화면이 full screen이 아니었다는 관찰은 P1-06에 기록했다.

### P0-04 Herdr patch 유지·업데이트 경로

`todo`. 조건부 입력은 fork `psw7205/herdr`의 `mobile-binding` branch와 설치된 patched binary에 의존한다. upstream PR 경로가 없어 fork를 소유·유지한다(ADR-036, runbook §8).

2026-09-28 fork `master`를 upstream과 sync하고 `mobile-binding`을 공개했다. stock `0.9.1`에는 API가 없고 두 binary의 version 문자열이 같다. `/api/sessions`의 `herdr.conditional_input`(`supported`/`unsupported`/`unknown`), `doctor`의 `conditional_input`·blocker, Web UI 안내로 감지를 노출하며 Bridge는 fail closed한다. build·백업·설치·live handoff·rollback·upgrade 절차는 [Herdr patch runbook](../herdr-patch.md)에 기록했다.

stock server에서의 `unsupported` 판정, rollback, 새 release rebase, 실행 중 Claude session이 patch→stock 전환에서 `unverified`로 남고 patch 복구 후 `active`로 돌아오는 P0-07 scenario는 격리 환경에서 실행하지 않았다. P0-07과 연결된다.

후속 patch 후보: `pane.read` 응답에 frame과 같은 lock에서 읽은 실제 PTY cols/rows를 넣는다(P1-05).

### P0-07 Binding·capability 손실과 agent 종료 구분

`done (2026-09-24)`, P0-04와 연결.

근거: fake gateway를 쓴 `internal/session` unit test(`TestBindingLossKeepsSessionUnverified`, `TestBindingLossEndsOnlyOnConfirmedEnd`, `TestBindingRecovery`, `TestVerifiedObservationWinsOverContinuity`, `TestRejectedBindingForAnotherNativeEndsContinuity`, `TestPaneItemSupersededByVerifiedNativeSession`, `TestPaneItemSupersededBySessionMovedToItsPane`, `TestEndedSessionRevivesOnVerifiedBinding`, `TestAmbiguousPaneGetsNoContinuity`, `TestSameNativeVerifiedOnTwoPanesIsAmbiguous`, `TestDuplicatedPaneSupersedesNothing`, `TestConcurrentRefreshAndReads`)와 `web/src/api.test.ts`. 실제 stock binary 실행과 App의 successor 전환 render test는 아니며 P0-04의 격리 환경 검증에 남는다.

binding·capability를 잃은 `claude:<native-id>` session은 lifecycle `unverified`로 남고 `runtime_binding`을 비워 prompt·interrupt·Terminal을 fail closed한다. 같은 `pane_id`에서 claude가 보고되고 `agent_session`과 성공한 `agent.binding`의 native ID가 없거나 같을 때만 유지하며, 검증된 관찰이 먼저 ID를 차지하고, 한 pane에 active item이 둘이면 유지하지 않는다.

한 Refresh에서 같은 native ID가 여러 pane에서 검증되면 어느 관찰도 binding과 함께 `claude:<native-id>`를 차지하지 않아 기존 item은 `unverified`, 새 pane은 `unbound`로 남고 supersession도 없다(ADR-034).

native session을 식별하지 못한 `pane:<pane-id>` item은 binding이 없으면 `unbound`이고, 같은 pane에서 `claude:<native-id>`가 새로 검증되면 `superseded`+`successor_id`로 바뀌며 Web UI가 successor로 전환한다.

남은 한계: `agent_session`이 없으면 한 refresh 안의 같은 pane claude→claude 교체를 구분하지 못하고, agent 감지가 순간적으로 빠지면 `ended`가 된다. supersession은 pane 단위라 같은 pane의 process 교체도 `superseded`로 보인다.

### P0-08 Tailnet 설정 자동 감지·진단

`done (2026-09-24)`. `-tailnet-host`·`-tailnet-login` 기본값 `auto`, Origin 자동 추가, `doctor`의 `tailnet` section을 구현했다.

근거: P0-01에서 Serve 활성화 뒤 `doctor`가 `tailnet.issues: []`를 보고했고, 자동 감지된 host·login으로 소유자 phone의 Tailnet 접속이 수락됐다(P0-02). 남은 mobile 확인은 P0-02에서 추적한다.

알려진 한계: host·login은 시작 시 한 번만 해석한다. 예를 들어 Tailscale이 Running이 되기 전에 `launchd`로 부팅된 Bridge는 재시작 전까지 localhost 전용이며, background 재해석은 구현하지 않았다. 원격 경로는 Tailscale Serve만 유지한다(ADR-024).


완료 기준 원문: 새 package `internal/tailnet`이 `tailscale.com` 의존성 없이 CLI를 shell-out한다. `-tailnet-host`·`-tailnet-login` 기본값 `auto`는 시작 시 한 번 `tailscale status --json`에서 host(`Self.DNSName`의 trailing dot 제거)와 node 소유 사용자의 login을 얻는다. `-tailnet-host off`는 Tailnet 요청을 받지 않는 localhost 전용이고, 명시 값은 `auto`보다 우선한다. tagged node이거나 login이 없으면 host는 유지하고 login은 비워 모든 tailnet 요청을 거부한다. CLI 없음·실행 실패·Tailscale 미실행이면 `auto` host는 localhost 전용으로 동작하고, host를 명시했는데 `auto` login만 실패하면 host를 유지한 채 모든 tailnet 요청을 거부한다. 어느 쪽이든 Bridge는 종료하지 않는다. `-tailscale-bin`은 CLI 경로를 명시하고, 없으면 PATH, 이어서 알려진 설치 경로를 찾는다(`launchd`의 최소 PATH, macOS app bundle wrapper 대응). host와 owner login이 모두 확정된 경우에만 `https://<host>`를 Origin allowlist에 자동 추가한다. `-origins`는 기본값을 대체하므로 localhost 접속이 필요하면 `http://127.0.0.1:8787`도 직접 포함한다. login이 없는 동안 `-origins`의 tailnet HTTPS Origin은 경고 log와 함께 제외한다. 시작 log에 확정된 host/login과 각각의 출처를 남긴다. `doctor`에 `tailnet` section(`cli_available`, `backend_state`, `host`, `login`, `https_certificates`, `serve_proxy`, `funnel`, 조치 방법을 담은 `issues`)을 추가한다. `serve_proxy`는 Serve가 `<host>:443`을 Bridge listen 주소로 proxy하는지 확인한다. Funnel 감지는 모든 port와 `--bg` 없이 실행한 foreground 설정을 포함하며 활성화는 top-level blocker이고, Tailnet 미설정은 localhost 전용이 지원 범위이므로 blocker가 아니다. README는 설치자 기준 절차를 먼저 안내한다. Serve 활성화 뒤 `doctor`의 `tailnet.issues`가 비고 P0-01/P0-02 흐름으로 실제 접속된다.

### P0-09 공개 전 점검

`todo`, 소유자 주도. GitHub에 공개해 다른 사용자가 자신의 tailnet에 설치하기 전 단계다.

2026-09-28 공개 전 review에서 찾은 결함을 고쳤다: Serve가 client `Host`를 그대로 넘겨 tailnet host 미확정 시 loopback Host 위조 요청이 통과하던 우회(`internal/httpapi`, ADR-024), clickjacking·`nosniff` header와 static directory listing, 종료된 session의 transcript watcher fd 누수(`internal/session`, ADR-008), Bridge로 향하는 Serve raw TCP forward의 `doctor` blocker(`internal/tailnet`), service worker cache의 build id 자동 갱신.

Apache-2.0 LICENSE를 추가하고 Go module을 `github.com/psw7205/herdr-remote`로 바꿨다. commit author email은 소유자 GitHub 계정의 공개 email이라 유지한다. history의 host 보안 상태 서술은 2026-09-28 `git filter-repo`로 일반 조건 문장으로 치환했다. 2026-09-28 `herdr` patch branch를 fork에 공개했다(ADR-036). 같은 날 공개 전 문서 review로 PRD·ADR·architecture·README의 미구현 기능 서술을 현재 구현과 구분했다.

남은 것: repo를 공개한다.

### P1-05 Terminal mobile 조작·화면 크기

`todo`. `pane.read` visible ANSI frame을 xterm.js에 교체 표시하고 Herdr PTY resize는 호출하지 않는다.

Herdr 공개 API에는 정확한 PTY cols가 없어(rows는 `pane.get`/`session.snapshot`의 `scroll.viewport_rows`뿐) Bridge가 read-only `pane.layout`의 pane `rect`(zoomed tab의 focused pane이면 tab `area`)를 terminal frame 응답의 additive `cols`·`rows`로 넣는다([integration findings](integration-findings.md#2026-09-25--terminal-grid-크기-조사), ADR-035). rect는 border·scrollbar cell을 포함한 상한이고 direct attach resize lock이 있으면 PTY와 다를 수 있다.

Web의 xterm 로컬 grid는 Bridge 값과 frame의 줄 수·가장 긴 visible line 폭 중 큰 값이다(`web/src/terminalSizing.ts`). grid가 크면 빈 cell만 남고 작으면 줄바꿈이 깨지므로 큰 쪽을 택하며, Bridge 값이 없으면 최근 5개 poll 크기 중 최댓값을 쓴다(Bridge 값이 있던 poll은 그 grid로 기록해 layout 조회 한 번의 실패로 줄지 않는다). 폭은 xterm 6.0.0 기본 Unicode V6 폭표와 같게 센다.

Bridge는 `pane.layout`을 500ms 전용 timeout으로 읽고, 실패하면 `cols`·`rows` 없이 frame을 반환한다. Web은 1초 poll을 한 번에 하나만 보낸다.

fontSize는 화면 폭에 맞춰 6–16px에서 고르고 6px 미만이 필요하면 6px에서 horizontal scroll한다. ResizeObserver가 fit을 다시 계산하고, xterm이 pinch를 삼킬 수 있어 `A−`/`맞춤`/`A+` 버튼을 둔다. layout은 `100dvh`+safe-area, keyboard는 `visualViewport`로 key bar를 위에 두며 pinch zoom(`scale > 1`) 중에는 갱신하지 않는다.

근거(2026-09-25, headless Chrome 390×844): 변경 전 terminal 폭 867px에 줄바꿈이 틀렸고, 변경 후 6px에서 128×40 전체가 올바른 줄바꿈으로 보이며 약 90px의 horizontal scroll이 남는다. landscape는 9px에서 fit되고 높이 500px에서도 key bar가 보인다.

남은 것(실제 phone 미검증): iOS keyboard와 key bar, pinch, 6px 가독성, IME, Esc/Tab/arrows/Ctrl-C/Enter, horizontal scroll.

Herdr patch 권고(P0-04 범위): `pane.read` 응답에 frame과 같은 lock에서 읽은 실제 PTY cols/rows를 넣는다. 현재는 `pane.read`와 `pane.layout`이 별도 호출이라 그 사이 resize를 구분하지 못한다.

2026-09-25에는 header·key bar·글자 크기 버튼을 공통 token과 icon으로 바꾸고 두 theme에서 모두 어둡게 고정했다. 이 고정은 production build의 light theme에서 풀려 있었고 2026-09-27에 고쳤다([검증 기록](#배색-교체-검증-2026-09-27)). keyboard 보정은 화면 틀 전체로 옮겼다(P1-09). 크기 계산과 polling은 바꾸지 않았다(fixture 세로·가로 확인).

### P1-06 PWA 실제 기기 복구

`todo`, P0-02 이후. manifest, 192/512 PNG, service worker와 foreground reconnect는 구현했다. 홈 화면 설치와 화면 잠금·네트워크 전환 후 복구는 실기기에서 확인하지 않았다.

2026-09-24 관찰: phone에서 화면이 full screen이 아니었다. 2026-09-25 headless Chrome 390×844에서는 P1-05 변경 전에도 layout 높이가 viewport와 같아 재현되지 않았다. 가장 유력한 원인은 설치된 PWA가 아닌 browser tab 실행이라 manifest의 `display: standalone`이 적용되지 않은 것(설치된 PWA에만 적용)이다. Terminal 고정 grid가 phone 폭을 넘던 문제는 P1-05에서 fit으로 바꿨다. 설치된 PWA full screen은 실기기에서 미검증이다.

2026-09-25에는 새 palette의 icon, manifest 색, light/dark `theme-color`로 바꾸고 service worker cache 이름을 올렸다. 2026-09-27 배색 교체(P1-11)로 icon과 색을 다시 바꾸고 cache를 `herdr-chat-shell-v3`로 올렸다.

2026-09-28부터 cache 이름은 build 산출물 hash로 자동 갱신되고 manifest·icon은 stale-while-revalidate로 받는다. 이미 설치한 PWA의 icon 갱신은 실기기에서 확인하지 않았다.

### P1-09 화면 틀·scroll·navigation

`todo`, 실기기 확인만 남음.

2026-09-25 구현: 화면 틀을 visual viewport에 고정해 header·연결 띠·composer는 그대로 두고 대화 영역만 scroll한다. 맨 아래에 있을 때만 새 메시지를 따라가고, 아니면 "최신으로" 버튼과 새 메시지 수를 보인다. keyboard는 Android에서 viewport의 `interactive-widget=resizes-content`로, iOS에서 visual viewport 보정으로 처리한다. session과 view를 hash에 반영해 Back이 Terminal → Chat → 목록 순서로 돌아간다. Terminal은 연 시점의 `runtime_binding`을 유지한다.

근거: headless 360·390·412폭의 위·중간·아래에서 header y=0, 문서 scroll 없음, 메시지 400개 render 약 550ms, live 새 메시지에서 읽던 위치 유지.

남은 것: Android 실기기 keyboard와 설치형 PWA, iOS. P0-02와 같은 실기기 세션에서 확인한다.

### P1-10 Chat 내용 정리

`done (2026-09-25)`. Claude adapter는 `isMeta` 기록, slash command·bash mode의 호출과 출력, background 작업 알림을 Chat message에서 빼고 graph node로만 남긴다. 붙여넣기 wrapper는 태그만 벗긴다. 목록에 없는 형식은 보인다.

근거: `internal/claude` test. 같은 시점 main code Bridge와 비교해 작업 session의 user message가 23개에서 2개로 줄었고 assistant message 수는 같았다. 최종 규칙 dry-run에서 로컬 transcript 41개 중 invalid는 0건이었다([integration findings](integration-findings.md#2026-09-25--claude-transcript의-비입력-user-record와-transcript-이동)).

`<system-reminder>` block과 synthetic `No response requested.`는 실제 구조를 확인하지 못해 규칙에 넣지 않았다.

### P1-11 Design foundation

`done (2026-09-25)`. `light-dark()` color token(본문 4.5:1, 상태 표시 3:1 이상), system font, Lucide icon, 화면별 CSS Module로 바꾸고 한 줄 CSS를 지웠다.

2026-09-27에 배색을 Herdr website의 warm neutral에서 cool graphite로 바꿨다. 회색은 accent hue 쪽으로 약간 기울이고, 주 버튼은 먹색으로 칠하며 blue는 link·focus·눌린 상태에만 쓴다. 상태 표시는 PC Herdr와 같은 어휘와 색(입력 필요 빨강, 작업 중 노랑 spinner, 완료 teal, 대기 green check)을 쓴다.

dev 전용 fixture page(`web/fixture.html`)로 모든 상태를 익명 sample로 재현한다.

근거: fixture screenshot(light/dark, 360·412폭), Web Interface Guidelines audit. audit 중 찾은 입력 지연(글자당 224–336ms)은 16ms로 고쳤다.
