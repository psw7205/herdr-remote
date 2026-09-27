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
| Herdr native process binding과 PTY queue 검증 | `herdr` repo의 `codex/mobile-binding` branch |

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
