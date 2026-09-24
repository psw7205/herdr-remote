# 첫 vertical slice 구현 및 검증 기록

2026-09-24 기준. 설계 기준은 `docs/prd.md`와 `docs/adr.md`, 실제 Herdr 조사 결과는
`docs/integration-findings.md`다. 최초 실행 계획은 이전 commit에 남아 있다.

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
화면이 full screen이 아니었던 관찰은 원인을 진단하지 않았으며 [backlog](backlog.md)
P1-05/P1-06에 남겼다.

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
소유자 검증과 exact browser Origin을 모두 적용한다. 추가로
network ACL/grant도 이후 최소 권한으로 좁히는 것이 좋다. 전역 tailnet policy 수정은 이
vertical slice의 변경 범위가 아니다. Serve는 2026-09-24에 Funnel 없이 활성화했다.
Bridge 재시작 복구는 사용자 `launchd` job으로 실행하던 때 확인했다. 이 job은 이후 상시 실행이
아니라 필요할 때만 시작하도록 바꿨으며, 실행 방식은 Git 밖의 host 설정이다.
