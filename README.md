# Herdr Remote

기존 Herdr agent session을 mobile Chat으로 투영하는 client/Bridge를 개발한다.
현재는 **읽기 전용 Herdr gateway와 integration 진단 명령**만 구현했다.
Chat, prompt 전송, transcript watcher, WebSocket, Terminal fallback은 아직 구현하지 않았다.

설계 기준은 [PRD](docs/prd.md)와 [ADR](docs/adr.md)다.
[Integration 조사](docs/integration-findings.md)에 실제 Herdr 0.9.1의 제약을,
[구현 계획](docs/implementation-plan.md)에 남은 작업과 acceptance 조건을 기록한다.

## 검증

`mise.toml`에 Go 1.25.14, Node 24.19.0, pnpm 10.33.2를 고정했다.
현재 Go 코드는 standard library만 사용한다. frontend와 외부 Go dependency는 해당 기능 구현 시 추가한다.

```sh
mise install
mise run test
mise run vet
```

## 실행 중 Herdr 진단

`herdr status server`로 실제 public API socket을 확인한다. 자동으로 server를 시작하거나
focused pane을 제어하지 않는다. socket은 `-socket` 또는 `HERDR_SOCKET_PATH`로 명시한다.

```sh
mise exec -- go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
```

명시한 socket이 없거나 server 조회가 실패하면 종료 코드가 nonzero다.
JSON report의 `blockers`와 `write_enabled`를 확인한다. 종료 코드 0은 **읽기 진단 성공**이며
vertical slice 완료나 write capability 지원을 의미하지 않는다. native association은 Herdr의
보고 여부일 뿐 transcript resolution 완료를 뜻하지 않는다.

`doctor`는 기존 session snapshot과 명시한 pane의 process 정보만 조회한다.
prompt/interrupt/resize/attach/start/resume은 호출하지 않는다. transcript 내용과 native session ID를
진단 출력에 포함하지 않는다. 각 socket 요청은 최대 5초, 응답은 최대 4 MiB로 제한한다.

## 현재 확인한 상태

2026-09-22에 Herdr 0.9.1 / protocol 22에서 기존 Claude pane과 foreground process 1개를 조회했다.
그 당시 Herdr native session association은 없었다. 상태는 실행할 때마다 달라질 수 있다.

`go test -race ./...`는 socket framing, identity 보존, 누락 association, 잘못된 pane/response ID,
malformed/oversized response, API error, cancellation, 진단 결과를 검증한다.
이 테스트는 실제 prompt handoff 또는 stale write 차단을 증명하지 않는다.
