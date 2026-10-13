# 문서 구조

`docs/` 바로 아래에는 공개할 현재 기준 문서를 둔다. 로컬 조사·실측 상세는 Git에서 제외한 `docs/records/`에 보관한다.
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

로컬 기록은 commit·push 대상이 아니며 다른 checkout에 존재한다고 가정하지 않는다. 기록은 날짜별로
추가하고 실측·unit test·미확인을 구분한다. 현재 설치 버전의 지원 여부는 code와 runtime에서 다시 확인한다.

공개할 결론은 code·test, ADR, architecture 또는 runbook에 반영한다. Commit·issue의 검증 요약에는
실행한 명령과 결과만 남기고 개인 경로, 계정, host의 실제 설정과 보안 상태를 복사하지 않는다.
공개 문서는 로컬 기록을 필수 근거나 링크 대상으로 삼지 않는다.
