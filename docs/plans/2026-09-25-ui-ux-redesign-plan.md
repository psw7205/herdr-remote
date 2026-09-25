# UI/UX 개편 계획

2026-09-25 초안. 요구와 결정의 기준은 [PRD](../prd.md), [ADR](../adr.md), [Backlog](../backlog.md)다.
이 계획은 web client의 화면·상호작용과 Bridge의 Chat 표시 범위만 다룬다. Herdr lifecycle,
binding 검증, command receipt, Terminal passive mirror 불변식은 바꾸지 않는다.

## 목표

긴 대화에서도 header와 composer가 항상 보여야 한다. PC Herdr와 같은 상태 어휘로 "입력이 필요한
agent 찾기 → 응답 읽기 → 짧은 follow-up 또는 중단"을 phone에서 끝낼 수 있는 Sessions·Chat·Terminal을
만든다.

## 전제

- 주 기기는 소유자의 Android phone(Chrome, 설치형 PWA)으로 가정한다. iOS Safari는 호환 대상이지만
  실기기 검증 범위 밖이다.
- 단일 사용자 도구이며 desktop 전용 layout은 최적화하지 않는다(PRD §26).
- Backlog는 P0 잔여 항목을 먼저 하라고 추천하지만, 이 계획은 소유자 요청에 따라 UI를 앞세운다.
  어느 단계도 P0 불변식을 건드리지 않는다.

## 현재 진단

- **Scroll**: Chat 화면 틀이 viewport 높이를 고정하지 않아 대화 영역이 아니라 문서 전체가 scroll된다.
  메시지 21개인 session에서는 대화 영역이 내부 scroll 없이 약 17,000px로 늘어났다. 맨 아래를 볼 때
  header는 viewport 위 약 16,600px에 있었다. 중간을 읽는 동안에는 header와 composer(중단 포함)가
  모두 화면 밖이다. 새 메시지마다 맨 아래로 이동하는 동작도 있어서 틀만 고치면 읽던 위치를 빼앗는다.
- **내용**: Claude Code의 local command 기록, meta 안내, skill 주입 본문이 "나" 메시지로 표시된다.
  실제 session 하나에서 "나" 메시지 18개 중 17개가 이런 기록이었다. meta 표시가 없는 형식도 섞여
  있어서 그 표시만으로는 거를 수 없다.
- **시각 체계**: token 없이 한 줄로 압축된 CSS에 색 43종, radius 11종, font-size 10종이 흩어져 있다.
  본문 14px, label 10–11px, 뒤로 가기 target 31px이다. light/dark, focus 표시, reduced motion 대응이
  없다.
- **Generic 흔적**: 대문자 eyebrow, `·`로 이은 meta 문자열, 같은 모양의 rounded card 반복, 어두운
  배경에 밝은 민트 단일 accent, icon 대신 쓴 문자 glyph가 있다. 모두 `frontend-design`과 `impeccable`이
  AI 생성 UI의 기본값으로 꼽는 항목이다.
- **구조**: 목록은 상태별로 묶이지 않고 색 점으로만 상태를 구분하며, 완료와 대기가 같은 색이다.
  마지막 activity와 메시지는 표시하지 않는다(P1-03). Terminal 전환은 URL과 history에 남지 않아서
  Android Back이 Chat을 건너뛰고 목록으로 간다. 로딩 중에도 "agent 없음"이나 "대화 없음" 빈 상태가
  먼저 보인다.
- **Copy**: "native transcript", "conditional input API", "command ID" 같은 내부 용어가 사용자 안내에
  나온다. 작업 중인 agent 앞에서 composer는 "Terminal에서 진행하세요"라고 안내한다.

## 참고 기준

| 출처 | 채택 | 제외 |
| --- | --- | --- |
| `pbakaus/impeccable` | 기본 관점. 목록·입력은 Operate mode, 응답 읽기는 Read mode다. craft floor(상태별 표현, contrast, browser 기본 surface 테마)와 Android 기준(System Back, 48dp target, IME inset)을 따른다 | 설치와 launcher 실행. 외부 binary를 받아 실행하고, repo root의 PRODUCT.md·DESIGN.md가 기존 PRD와 문서 배치 규칙에 겹친다 |
| `anthropics/skills` frontend-design | generic 기본값 점검표. token 초안 → brief 대조 → 구현 → screenshot critique 순서와 UI copy 원칙을 따른다 | 과감한 미적 모험. Operate mode에서는 익숙함이 우선이다 |
| Web Interface Guidelines(설치된 `web-design-guidelines`) | 구현 후 audit gate로 쓴다. focus, reduced motion, 색 scheme, URL 상태, 긴 목록, touch, 시각 표기를 본다 | 영문 전용 규칙(Title Case 등) |
| `vercel/ai-elements` | Conversation(하단 고정, 최신으로 이동), PromptInput(보내기↔중단), CodeBlock(복사)의 구성을 참고한다 | 의존성 도입. Next.js·AI SDK·shadcn·Tailwind를 전제한다 |
| shadcn/ui·Tailwind | — | 1k줄 thin client에서 styling stack을 바꾸는 비용이 효과보다 크다 |
| `ui-ux-pro-max`, `taste-skill` | 다른 기준과 겹치는 점검 항목만 쓴다 | marketing surface 기법(grain, glass, parallax 등). "전체 높이는 최소 높이로"라는 권고를 내부 scroll 화면에 적용하면 이번 버그가 그대로 생긴다 |
| `herdr` repo | visual authority. website palette(warm neutral, 단일 blue accent, 2–6px radius, 전용 dark palette)와 TUI 상태 표시(blocked 빨강, working 노랑 spinner, done teal, idle green check)를 따른다 | — |

## 디자인 방향

- **장면**: PC를 떠난 소유자가 짧게 확인하고 입력한다. 낮 실외와 밤 모두 쓰므로 system light/dark를
  따른다. Terminal은 두 theme에서 모두 어둡게 유지한다.
- **원칙**: 도구는 작업 속으로 사라져야 한다. 표현보다 익숙함을 우선하고, 대담함은 상태 표시 한
  곳에만 쓴다.
- **정체성**: 현재의 navy와 민트를 버리고 Herdr 본체를 따른다. warm neutral surface 2단계, 단일 blue
  accent, 작은 radius를 쓴다. light theme에서 이 blue를 글자색으로 쓰면 contrast가 약 2.4:1이므로
  text용 진한 변형을 따로 둔다.
- **상태**: PC Herdr와 어휘와 색을 맞춘다. 입력 필요는 빨강, 작업 중은 노랑과 움직이는 표시, 확인하지
  않은 완료는 teal, 대기는 green check로 하고 오류도 구분한다. 색만으로 구분하지 않고 모양과 label을
  함께 쓴다.
- **Type**: system sans 한 가지(Android는 Roboto·Noto Sans KR, iOS는 SF·Apple SD Gothic Neo)와
  code용 system mono를 쓴다. 본문 16px 기준의 고정 rem scale(단계 비율 1.125–1.2)을 쓰고, label은
  최소 12px로 한다.
- **Token**: 색은 역할(surface 2단계, ink, muted, line, accent, 상태)별로 light와 dark를 정의한다.
  radius, spacing, motion(150–250ms, reduced motion 존중)도 scale로 두고, component는 raw 값을 쓰지
  않는다.
- **Icon**: drawn icon set 하나로 문자 glyph를 대체한다.

## 화면 구성

- **Sessions**: 상단 app bar에 제목과 전체 연결 상태를 둔다. card를 나열하지 않고 입력 필요 / 작업 중
  / 완료 / 대기 순의 grouped list로 보여 준다. PRD §7.1의 3개 group(Needs Attention, Working, Recent)을
  Herdr의 done/idle 구분에 맞춰 4개로 나누므로, 승인되면 PRD도 고친다. 각 행은 제목, project, 상태
  label, 마지막 activity, 마지막 메시지 한 줄을 표시한다. pane ID 같은 Herdr 내부 값은 보조 정보로 내린다. 로딩 중에는
  skeleton을 보이고, 빈 상태에서는 다음 행동을 안내한다.
- **Chat**: 고정 header에 뒤로, 한 줄 제목, 상태, Chat·Terminal 전환을 둔다. 연결 문제는 있을 때만
  띠로 보인다. assistant 응답은 bubble 없는 전폭 본문, user 입력은 오른쪽 compact bubble로 표시하고
  메시지마다 붙은 이름표는 없앤다. 읽는 동안에는 위치를 지키고, 새 메시지는 "최신으로" 버튼으로
  알린다.
- **Composer**: 고정 영역에 자동 높이 입력, 상태에 따라 보내기와 중단을 오가는 주 버튼, 전달 상태 한
  줄을 둔다. 작업 중에는 끝나면 입력할 수 있다는 안내와 중단을 보인다. 입력 필요 상태에서는 Terminal
  열기를 보인다.
- **Terminal**: Chat과 같은 header와 전환을 쓴다. 글자 크기 버튼과 특수 키 bar만 icon과 token으로
  맞추고, 크기 계산과 polling 동작은 바꾸지 않는다(P1-05).
- **Navigation**: session과 view를 URL에 반영해서 Back이 Terminal → Chat → Sessions 순으로 돌아가게
  한다. successor 전환 동작은 유지한다.

## 순서

1. **화면 틀과 scroll** (신규 항목): header, 연결 띠, composer를 고정하고 대화 영역만 scroll한다. 맨 아래
   근처일 때만 새 메시지를 따라가고, 아니면 "최신으로" 버튼을 보인다. 첫 진입은 곧바로 맨 아래로
   간다. Android keyboard가 열려도 composer가 보여야 한다. Chrome은 기본 설정에서 keyboard가 layout
   viewport를 줄이지 않으므로 방식을 하나 고른다. 하나는 viewport 설정으로 content resize를 opt-in하는
   것이고, 다른 하나는 Terminal에 있는 visual viewport 보정을 화면 틀 전체로 넓히는 것이다. 앞의 방식은
   Terminal의 기존 보정과 겹치므로 Terminal도 함께 확인한다. 외형과 Terminal 크기 계산은 바꾸지 않는다.
   **검증**: headless 390×844에서 긴 대화의 위·중간·아래 모두 header가 y=0에 있고 대화 영역이 내부
   scroll되는지 측정한다. Android 실기기에서 keyboard를 연 상태로 두 방식 중 하나를 확정한다. 계획
   전체의 전제가 여기서 확인된다.
2. **Chat 내용 정리** (Bridge, 신규 항목): Claude adapter의 projection에서 사용자가 쓰지 않은 기록
   (local command, meta 안내, skill 주입)을 Chat message에서 뺀다. 알려진 형식만 좁게 빼고, 모르는
   형식과 사용자의 실제 입력(붙여넣기 wrapper 포함)은 남긴다. 판정은 web이 아니라 Bridge에서
   한다(ADR-011). **검증**: 실제 구조를 확인한 익명 fixture로 Go test를 쓰고 `mise run test`,
   `mise run vet`를 통과시킨다. 실제 session의 "나" 메시지가 실제 입력만 남는지 확인한다.
3. **Foundation과 시안 승인**: token(light/dark), 기본 surface(색 scheme, focus, text selection, tap
   feedback), icon set, component별 CSS 분리를 만든다. 실제 transcript 없이 모든 상태(입력 필요,
   작업 중, 연결 확인 불가, 연결 끊김, 전달 불확실, 긴 code와 표)를 재현하는 dev 전용 익명 fixture
   화면도 만든다. 그 위에서 Sessions와 Chat을 light/dark, 360·412px 폭으로 보여 주고 방향을
   승인받는다. 승인 후 PWA 색, manifest, icon, service worker cache를 새 palette에 맞춘다.
   **검증**: 소유자 시안 승인, 두 theme의 contrast(본문 4.5:1, UI 3:1), `pnpm --dir web test`,
   `pnpm --dir web build`.
4. **Session 화면** (P1-04, P1-06 일부): 공통 header, view 전환과 URL 상태, 메시지 표현, code block
   복사와 가로 scroll, 상태별 composer 안내, notice 등급과 CTA, 사용자 언어 copy를 만든다.
   **검증**: fixture의 모든 상태를 screenshot으로 보고 Web Interface Guidelines audit을 한다. 소유자가
   지정한 session에서 prompt를 한 번 보내 기존처럼 정확히 한 번 도착하는지 확인하고, Android Back
   순서도 확인한다.
5. **Sessions 목록** (P1-03): 상태 group, 상태 표시, skeleton, 빈 상태, 연결 상태를 만든다. 마지막
   activity와 메시지에는 Bridge의 additive Session field가 필요하다. 이 값은 transcript 시각에서
   얻고 terminal text로 추측하지 않는다(ADR-018). 2단계 이후에 해야 preview에 noise가 섞이지 않는다.
   **검증**: fixture 상태별 screenshot을 본다. API를 바꾸면 Go test를 추가하고 관련 문서를 갱신한다.
6. **Terminal 정렬** (P1-05 최소): header와 전환, key bar와 글자 크기 icon, token을 적용한다.
   **검증**: 기존 terminal sizing test를 통과시키고 Chat↔Terminal 전환 전후 pane geometry가 같은지
   확인한다.
7. **마감 점검**: Web Interface Guidelines와 craft floor로 전체를 audit한다. 설치형 PWA를 Android
   실기기에서 확인하고, README와 backlog 상태를 갱신한다.

1·2단계는 서로 독립이라 먼저 배포할 수 있다. 4–6단계는 3단계 시안 승인을 전제로 한다.

## 리스크

- **Keyboard와 viewport**: headless는 Android Chrome의 keyboard와 설치형 PWA 동작을 재현하지 못한다.
  1단계의 두 방식 중 어느 쪽이 맞는지는 실기기에서만 확정된다. viewport 설정 방식은 iOS Safari에서
  동작하지 않으므로 그곳에서는 visual viewport 보정이 fallback으로 남는다. iOS는 기기가 없어 미검증으로
  남는다.
- **Scroll anchoring**: Markdown과 code block이 다시 그려지면서 높이가 바뀌면 위치가 튈 수 있다.
  메시지가 수백 개인 session의 render 비용도 확인해야 한다.
- **Filter 오판**: 과하게 거르면 실제 입력이 사라진다. Claude Code version마다 기록 형식이 바뀔 수
  있으므로 모르는 형식은 보이는 쪽으로 둔다.
- **범위 팽창**: tool card, Changes, 알림, 검색(P2)은 제외한다. slash command를 status event로
  보이는 일은 protocol 추가라서 이번 범위가 아니다.
- **Privacy**: 실제 session의 screenshot은 추적하지 않는 위치에서만 본다. fixture는 익명 sample로
  만든다.

## 열린 결정

| 결정 | 추천 | 이유 |
| --- | --- | --- |
| Theme | system light/dark | 낮 실외와 밤 모두 쓰고 두 OS 모두 두 theme을 기본으로 다룬다. fixture 화면이 검증 부담을 줄인다 |
| Styling | vanilla CSS token과 component별 CSS | dependency나 build 변경이 없고 1k줄 client 규모에 맞는다. Tailwind·shadcn으로 가면 markup을 전면 재작성해야 한다 |
| Font | system stack | Operate mode에 맞고 offline shell과 외부 request에 영향을 주지 않는다. 한글 web font를 쓰려면 self-host와 cache 추가가 필요하며, 외부 CDN은 어느 경우에도 쓰지 않는다 |
| Icon | Lucide | stroke가 일관되고 쓰는 icon만 bundle된다. 직접 그린 SVG는 계속 관리해야 한다 |
| 목록 activity·preview field | 5단계에서 추가 | PRD §7.1과 P1-03 완료 기준이 요구한다 |
| slash command 결과 | 이번에는 숨김 | status event로 보이려면 protocol 추가와 ADR이 필요하다 |
| design skill 설치 | 설치하지 않음 | 필요한 기준은 이 계획에 반영했고 audit은 설치된 skill로 한다. 시안 critique이 부족하면 그때 `impeccable`을 검토한다 |

승인되면 1·2·3단계를 backlog의 신규 ID로 등록하고, 기존 P1-03~06 항목에는 이 계획을 연결한다.
