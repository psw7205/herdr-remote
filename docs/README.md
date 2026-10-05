# 문서 구조

`docs/` 바로 아래에는 현재 기준 문서를 두고, 날짜가 붙은 실측 근거는 `records/`에 둔다.
끝난 계획은 archive로 옮기지 않고 삭제한다. 이전 내용은 Git history에서 찾는다.

한 사실은 한 문서가 소유하고 다른 문서는 link한다. 동작 invariant는 [architecture.md §8](architecture.md#8-invariants),
agent 작업 규칙은 `AGENTS.md`, 남은 작업과 상태는 [GitHub Issues](https://github.com/psw7205/herdr-remote/issues)가 소유한다.

| 문서 | 성격 | 갱신 규칙 |
| --- | --- | --- |
| [prd.md](prd.md) | 제품 요구와 범위의 결정 snapshot | 제품 범위가 바뀔 때만 고친다. 구현 상태는 쓰지 않는다 |
| [adr.md](adr.md) | architecture 결정 | ADR 단위로 추가한다. 기존 ADR 본문은 고치지 않고 `Status`와 날짜 붙은 note로 정정한다 |
| [architecture.md](architecture.md) | 결정을 조합한 runtime 구조, data flow, invariant | ADR 변경이 구조에 영향을 주면 함께 고친다 |
| [herdr-patch.md](herdr-patch.md) | Herdr patch 운영 runbook | 절차가 바뀌면 현재 상태로 고친다. `cmd/doctor`가 이 경로를 참조한다 |
| `images/` | README 화면 캡처 | 익명 fixture(`web/fixture.html`)로만 만든다. 실제 session 화면은 넣지 않는다 |
| `plans/` | 진행 중인 작업 계획 | 작업이 끝나면 삭제한다. 남은 일은 issue로 옮긴다 |
| [records/integration-findings.md](records/integration-findings.md) | Herdr/Claude/Codex 코드·runtime 조사 | 날짜별 section으로 추가한다 |
| [records/verification.md](records/verification.md) | 실제 검증 결과 | 날짜별 section으로만 추가하고 실측·unit test·미확인을 구분한다 |

`records/`의 내용은 기록 시점의 관찰이다. 현재 설치된 Herdr·agent version의 지원 여부로
단정하지 않고 code와 runtime에서 다시 확인한다.
