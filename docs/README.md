# 문서 구조

`docs/` 바로 아래에는 현재 기준 문서를 두고, 날짜가 붙은 실측 근거는 `records/`에 둔다.
끝난 계획은 archive로 옮기지 않고 삭제한다. 이전 내용은 Git history에서 찾는다.

| 문서 | 성격 | 갱신 규칙 |
| --- | --- | --- |
| [prd.md](prd.md) | 제품 요구와 범위 | 요구·범위가 바뀌면 현재 상태로 고친다 |
| [adr.md](adr.md) | architecture 결정 | ADR 단위로 기록하고 각 ADR의 `Status`로 상태를 표시한다 |
| [architecture.md](architecture.md) | 결정을 조합한 runtime 구조, data flow, invariant | ADR 변경이 구조에 영향을 주면 함께 고친다 |
| [backlog.md](backlog.md) | 남은 작업, 우선순위, 완료 기준 | 기존 ID와 의존 관계를 유지한다 |
| [herdr-patch.md](herdr-patch.md) | Herdr patch 운영 runbook | 절차가 바뀌면 현재 상태로 고친다. `cmd/doctor`가 이 경로를 참조한다 |
| [records/integration-findings.md](records/integration-findings.md) | Herdr/Claude/Codex 코드·runtime 조사 | 날짜별 section으로 추가한다 |
| [records/verification.md](records/verification.md) | 구현 경계와 실제 검증 결과 | 검증 날짜와 실측 여부를 구분해 추가한다 |

`records/`의 내용은 기록 시점의 관찰이다. 현재 설치된 Herdr·agent version의 지원 여부로
단정하지 않고 code와 runtime에서 다시 확인한다.
