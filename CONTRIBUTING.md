# 개발 가이드

이 문서는 로컬 개발과 검증 절차를 설명한다. 제품 경계는 [AGENTS.md](AGENTS.md),
요구·결정은 [PRD](docs/prd.md)와 [ADR](docs/adr.md)를 따른다.

## 환경 준비

`mise.toml`의 Go/Node.js/pnpm version을 사용한다. 설치·기본 실행은
[README](README.md#빠른-시작)를 따른다. `web/pnpm-lock.yaml`을 유지하고 다른 package manager의
lockfile을 추가하지 않는다. Go dependency를 바꾸면 `go.mod`와 `go.sum`을 함께 확인한다.

Bridge는 시작할 때 `web/dist/index.html`을 검사한다. Vite 개발 모드에서도 최초에 한 번
`mise exec -- pnpm --dir web build`를 실행해야 한다.

## 개발 server

기존 Herdr server가 실행 중이고 필요한 API를 제공해야 한다. 첫 terminal에서:

```sh
export HERDR_SOCKET_PATH="$(herdr status server | sed -n 's/^socket: *//p')"
test -S "$HERDR_SOCKET_PATH"
mise exec -- go run ./cmd/bridge -herdr-socket "$HERDR_SOCKET_PATH" \
  -origins "http://127.0.0.1:8787,http://127.0.0.1:5173"
```

다른 terminal에서:

```sh
mise exec -- pnpm --dir web dev --strictPort
```

`http://127.0.0.1:5173`으로 접속한다. Vite는 `/api`와 WS를 `127.0.0.1:8787`로 proxy한다.
`localhost`와 `127.0.0.1`은 다른 Origin이므로 주소와 allowlist를 일치시킨다.
기존 Bridge가 port를 사용 중이면 해당 process/service를 확인한 뒤 개발용 instance와 조정한다.
Herdr server나 사용자 agent를 종료해서 port 문제를 해결하지 않는다.

### Fixture 화면

Herdr와 Bridge 없이 UI 상태를 확인할 때는 Vite만 띄우고 `http://127.0.0.1:5173/fixture.html`에
접속한다. `?scenario=`로 `long`, `live`, `disconnected`, `delivery-unknown`, `rejected`, `empty`,
`herdr-down`, `unsupported`, `slow`, `superseded`를 고를 수 있다. `/`로 시작하는 입력은 transcript에서
숨는 slash command처럼 echo 없이 수락된다. 이 page는 fetch와 WebSocket만 가짜 Bridge로 바꾸고
실제 client code를 그대로 실행한다. 데이터는 `web/src/fixtures/`의 익명 sample뿐이며 production
build에는 포함되지 않는다. 실제 session으로 화면을 확인할 때는 screenshot을 추적 파일에 남기지 않는다.
dev server의 CSS는 build처럼 낮춰지지 않는다(`light-dark()` 등). 색이나 theme을 바꾸면 build한
화면에서도 확인한다.

## 변경별 검증

| 변경 | 실행할 검증 |
| --- | --- |
| Go Bridge | `mise run test`, `mise run vet` |
| React/TypeScript/PWA | `mise exec -- pnpm --dir web test`, `mise exec -- pnpm --dir web build` |
| protocol·두 영역 공통 변경 | 위 네 명령 모두 |
| 문서 | `git diff --check`, relative link·실제 명령·개인 경로 유입 확인 |
| Herdr 본체 | `herdr` repo가 요구하는 검증 및 affected runtime scenario |

`mise run test`는 `go test -race ./...`, `mise run vet`는 `go vet ./...`다.
Herdr가 없는 환경에서도 synthetic fixture 기반 unit/integration 테스트를 실행할 수 있다.
실제 socket 진단은 별도로 실행한다.

```sh
mise exec -- go run ./cmd/doctor -socket "$HERDR_SOCKET_PATH"
```

## 실제 handoff 검증

실행 전후의 pane, foreground process incarnation, native session identity를 기록한다.
사용자 transcript 원문을 test artifact나 Git에 저장하지 않는다.

1. 같은 cwd의 여러 session을 native ID로 구별한다.
2. 한 mobile prompt가 같은 process/session의 transcript에 user/assistant로 나타나는지 확인한다.
3. 같은 `command_id` retry와 stale binding을 보내 duplicate 또는 다른 process 입력이 없는지 확인한다.
4. WS/Bridge를 끊었다가 복구해 agent 지속, cursor epoch, replay 또는 새 snapshot을 확인한다.
5. unsupported interaction은 같은 Terminal에서 처리하고, mobile 화면 전환 전후 PTY geometry를 비교한다.

실행한 항목과 unit test로만 확인한 항목을 구분해 기록한다. 더 상세한 미완료 acceptance는
[Backlog P0](docs/backlog.md#p0--배포와-핵심-안정성)를 따른다.

## 문서와 변경 제출

작업 시작 시 관련 backlog ID와 성공 기준을 정한다. 작은 구현 선택은 code로 설명하고,
architecture 의미가 바뀔 때만 ADR을 수정한다. 실제 구현과 문서가 다르면 차이를 확인한 뒤
완료 상태를 갱신한다. 기능 아이디어를 구현 완료로 바꾸지 않는다.

검증된 논리 단위로 commit하고 사용자 변경을 함께 stage하지 않는다. commit subject는
`type(scope): 설명` 형식으로 작성한다. push는 명시 요청이 있을 때만 하며 main merge는
사용자와 합의된 절차를 따른다. 이 repo의 권한이 별도 `herdr` upstream 기여 권한을 뜻하지 않는다.
