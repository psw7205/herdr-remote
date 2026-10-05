// Anonymous sample data for the dev-only fixture page. Nothing here comes from
// a real transcript, pane or host; every name, path and message is invented.
import type { ConditionalInput, Delivery, Message, Session, StartCandidate, StartCapability, StartDelivery, ToolActivity } from '../api'
import { fixtureChanges, type FixtureChanges } from './changes'

export type FixtureSession = { session: Session; messages: Message[] }
export type Scenario = {
  name: ScenarioName
  sessions: FixtureSession[]
  conditionalInput: ConditionalInput
  command: Delivery
  // Milliseconds added to every HTTP answer.
  latency: number
  listFails: boolean
  bridge: { revision: string; modified: boolean; client_build: string }
  socketFails: boolean
  // Interval of unsolicited assistant messages on the working session; 0 is off.
  liveEvery: number
  // A pane: item that Herdr later re-keys to a verified claude: session.
  supersede?: { from: string; to: FixtureSession; after: number }
  // New session start (ADR-037): capability, folders and the start answer.
  start: { capability: StartCapability; candidates: StartCandidate[]; result: StartDelivery; latency: number }
  // Changed Files per session id; a session without an entry is a clean repo.
  changes: Record<string, FixtureChanges>
  // Timed transcript events on one session: new items and tool updates.
  script?: { session: string; steps: ScriptStep[] }
}
export type ScriptStep = { after: number; type: 'message.tool' | 'message.updated' | 'message.assistant'; message: Message; status?: Session['status'] }
export const scenarioNames = ['default', 'long', 'live', 'disconnected', 'delivery-unknown', 'rejected', 'empty', 'herdr-down', 'unsupported', 'slow', 'superseded', 'start-trust', 'start-unknown', 'no-root', 'codex', 'changes-large', 'tools', 'stale-client'] as const
export type ScenarioName = typeof scenarioNames[number]

const minute = 60_000
const at = (ago: number) => new Date(Date.now() - ago).toISOString()
let counter = 0
const message = (role: Message['role'], text: string, ago: number): Message => ({ id: `fixture-${++counter}`, role, text, timestamp: at(ago) })

// Like the Bridge, the preview is the newest text message; tool items have none.
export function lastText(messages: Message[]): (Message & { role: 'user' | 'assistant' }) | undefined {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const item = messages[index]
    if (item.role === 'user' || item.role === 'assistant') return item as Message & { role: 'user' | 'assistant' }
  }
}
function withPreview(session: Session, messages: Message[]): Session {
  const last = lastText(messages)
  if (!last) return session
  const text = last.text.replace(/\s+/g, ' ').trim()
  return { ...session, last_activity: last.timestamp, last_message: { role: last.role, text: text.length > 160 ? `${text.slice(0, 159)}…` : text } }
}

const base = { agent: 'claude', chat: true, terminal: true, active: true, lifecycle: 'active' } as const

const chartAnswer = `## 원인

차트 데이터가 **1만 건**을 넘으면 매 frame마다 전체 path를 다시 계산합니다. 확대할 때마다 같은 작업이 반복돼 main thread가 600ms 넘게 막힙니다.

| 단계 | 변경 전 | 변경 후 | 비고 |
| --- | --- | --- | --- |
| 첫 render | 1,840ms | 210ms | canvas로 전환 |
| 확대/축소 | 620ms | 48ms | 구간별 cache |
| memory | 312MB | 96MB | typed array 사용 |

## 변경

- 폭이 바뀔 때만 \`ResizeObserver\`에서 다시 계산합니다.
- 1만 건 이상은 LTTB로 줄인 뒤 그립니다.
- 확대 비율이 바뀌면 cache를 비웁니다.

\`\`\`ts
export function downsample(points: Point[], width: number): Point[] {
  const bucket = Math.ceil(points.length / width)
  if (bucket <= 1) return points
  const out: Point[] = []
  for (let start = 0; start < points.length; start += bucket) {
    out.push(largestTriangle(points, start, Math.min(start + bucket, points.length)))
  }
  return out
}
\`\`\`

> 참고: Safari는 \`OffscreenCanvas\`를 worker에서만 지원하므로 fallback을 남겼습니다.

전체 test는 아래 명령으로 돌렸습니다.

\`\`\`bash
pnpm --filter dashboard test -- --run src/charts/downsample.test.ts src/charts/cache.test.ts --reporter=verbose --coverage --coverage.include=src/charts/**
\`\`\`

자세한 근거는 [LTTB 설명](https://example.com/lttb)을 참고하세요. 캡처는 첨부하지 않았습니다. ![차트 비교](https://example.invalid/chart.png)`

const tool = (name: string, summary: string, state: ToolActivity['state'], input: object, result: string, ago: number, truncated = false): Message => {
  const id = `fixture-${++counter}`
  return { id, role: 'tool', text: '', timestamp: at(ago), tool: { id: `toolu_${id}`, name, summary, state, input: JSON.stringify(input, null, 2), input_truncated: false, result, result_truncated: truncated } }
}

const longSource = Array.from({ length: 220 }, (_, index) => `${String(index + 1).padStart(5)}→export const value${index} = compute(${index}, options)`).join('\n')

function toolConversation(): { messages: Message[]; running: Message } {
  const messages = [
    message('user', '결제 webhook이 가끔 두 번 처리돼. 원인 찾아서 고쳐 줘.', 30 * minute),
    message('assistant', '먼저 webhook 처리 경로와 중복 방지 로직을 확인하겠습니다.', 29 * minute),
    tool('Grep', 'idempotency', 'completed', { pattern: 'idempotency', path: '/workspace/sample-api/src' }, 'src/webhook/handler.ts:14\nsrc/webhook/store.ts:8', 29 * minute),
    tool('Read', 'src/webhook/handler.ts', 'completed', { file_path: '/workspace/sample-api/src/webhook/handler.ts' }, longSource, 28 * minute, true),
    tool('Read', 'src/webhook/store.ts', 'completed', { file_path: '/workspace/sample-api/src/webhook/store.ts' }, '    1→export async function remember(id: string) {\n    2→  await redis.set(id, 1)\n    3→}', 28 * minute),
    tool('WebFetch', 'docs.example.com', 'unknown', { url: 'https://docs.example.com/webhooks/retries', prompt: '재시도 간격을 요약해 줘' }, '', 27 * minute),
    tool('Bash', 'pnpm test -- webhook', 'error', { command: 'pnpm test -- webhook\npnpm lint', description: 'webhook test 실행' }, 'FAIL src/webhook/handler.test.ts\n  ✕ 같은 event를 두 번 받으면 한 번만 처리한다 (12 ms)\n\nExpected calls: 1\nReceived calls: 2', 26 * minute),
    message('assistant', '원인을 찾았습니다. 중복 확인과 기록 사이에 `await`가 있어서, 같은 event가 동시에 오면 둘 다 확인을 통과합니다.\n\n확인과 기록을 `SET NX` 한 번으로 합치겠습니다.', 25 * minute),
    tool('Edit', 'src/webhook/store.ts', 'completed', { file_path: '/workspace/sample-api/src/webhook/store.ts', old_string: 'await redis.set(id, 1)', new_string: "return (await redis.set(id, 1, 'NX')) === 'OK'" }, 'The file src/webhook/store.ts has been updated.', 24 * minute),
    tool('TodoWrite', '3 todos', 'completed', { todos: [{ content: '원인 확인', status: 'completed' }, { content: 'SET NX로 변경', status: 'completed' }, { content: '테스트 실행', status: 'in_progress' }] }, 'Todos have been modified successfully.', 24 * minute),
    message('assistant', '수정했습니다. 이제 test를 다시 실행합니다.', 2 * minute),
  ]
  const running = tool('Bash', 'pnpm test -- webhook', 'running', { command: 'pnpm test -- webhook', description: 'webhook test 다시 실행' }, '', minute)
  return { messages: [...messages, running], running }
}

function dashboardConversation(): Message[] {
  return [
    message('user', '대시보드 차트가 데이터가 많으면 너무 느려. 원인부터 찾아줘.', 26 * 60 * minute),
    message('assistant', '먼저 profiler로 render 경로를 확인하겠습니다. 차트 component와 data hook을 읽고 있습니다.', 26 * 60 * minute - 40_000),
    message('assistant', chartAnswer, 26 * 60 * minute - 6 * minute),
    message('user', '좋아. 그럼 cache 무효화 조건만 다시 설명해 줘.\n\n특히 확대/축소를 빠르게 반복할 때가 궁금해.', 95 * minute),
    message('assistant', '확대 비율(`scale`)과 표시 구간(`range`)을 key로 씁니다. 빠르게 반복하면 마지막 요청만 반영하고, 중간 결과는 버립니다.\n\n1. 입력이 들어오면 150ms debounce\n2. 같은 key면 cache hit\n3. 다른 key면 이전 계산을 취소', 94 * minute),
    message('user', '테스트까지 해줘', 12 * minute),
    message('assistant', 'test를 추가하고 전체를 실행하는 중입니다. `downsample`의 경계값(빈 배열, 폭 1px, 중복 좌표)을 먼저 확인합니다.', 11 * minute),
  ]
}

function longConversation(size: number): Message[] {
  const out: Message[] = []
  for (let index = 0; index < size; index += 1) {
    const ago = (size - index) * 3 * minute
    if (index % 2 === 0) out.push(message('user', `${index / 2 + 1}번째 요청: 다음 module도 같은 방식으로 정리해 줘.`, ago))
    else out.push(message('assistant', index % 10 === 1 ? chartAnswer : `정리했습니다. 바뀐 부분은 세 가지입니다.\n\n- 입력 검증을 한 곳으로 모았습니다.\n- 중복 요청을 막았습니다.\n- 실패하면 원인과 다음 행동을 안내합니다.\n\n다음 module로 넘어갈까요?`, ago))
  }
  return out
}

const terminalFrame = [
  '\x1b[1;36m╭──────────────────────────────────────────────────────────────╮\x1b[0m',
  `\x1b[1;36m│\x1b[0m \x1b[1mClaude Code\x1b[0m  sample-api${' '.repeat(38)}\x1b[1;36m│\x1b[0m`,
  '\x1b[1;36m╰──────────────────────────────────────────────────────────────╯\x1b[0m',
  '',
  '\x1b[33m●\x1b[0m Bash(pnpm test -- --run payments)',
  '  ⎿  테스트 DB를 초기화합니다. 계속할까요?',
  '',
  '   \x1b[1m1. 예\x1b[0m',
  '   2. 아니요, 다른 방법을 알려 주세요',
  '',
  '\x1b[2m↑/↓ 선택 · Enter 확인 · Esc 취소\x1b[0m',
].join('\r\n')

function sessions(): FixtureSession[] {
  counter = 0
  const attention = [
    message('user', '결제 API에 실패 case test를 보강해 줘.', 40 * minute),
    message('assistant', '실패 case 여섯 가지를 추가했습니다. 실행하려면 테스트 DB를 초기화해야 해서 권한 확인이 필요합니다. Terminal에서 선택해 주세요.', 2 * minute),
  ]
  const working = dashboardConversation()
  const done = [
    message('user', '배포 전에 확인할 항목을 정리해 줘.', 70 * minute),
    message('assistant', '배포 checklist를 정리했습니다.\n\n- [x] migration dry-run\n- [x] feature flag 기본값 확인\n- [ ] rollback 절차 공유\n\n마지막 항목은 담당자 확인이 필요합니다.', 18 * minute),
  ]
  const idle = [
    message('user', 'README를 영어로 옮겨 줘.', 5 * 60 * minute),
    message('assistant', '번역을 마쳤습니다. 용어는 기존 문서의 표기를 따랐습니다.', 4 * 60 * minute),
  ]
  const unverified = [
    message('user', '로그 수집기 설정을 정리해 줘.', 3 * 60 * minute),
    message('assistant', '설정 파일 세 개를 하나로 합치는 중입니다.', 170 * minute),
  ]
  return [
    { session: withPreview({ ...base, id: 'claude:fixture-attention', pane_id: 'w1:p1', project: '/workspace/sample-api', title: '결제 API 실패 case 보강', status: 'needs_attention', runtime_binding: 'fixture-binding-attention' }, attention), messages: attention },
    { session: withPreview({ ...base, id: 'claude:fixture-working', pane_id: 'w1:p2', project: '/workspace/sample-dashboard', title: '대시보드 차트 렌더링 개선', status: 'working', runtime_binding: 'fixture-binding-working' }, working), messages: working },
    { session: withPreview({ ...base, id: 'claude:fixture-done', pane_id: 'w2:p1', project: '/workspace/infra-notes', title: '배포 checklist 정리', status: 'completed', runtime_binding: 'fixture-binding-done' }, done), messages: done },
    { session: withPreview({ ...base, id: 'claude:fixture-idle', pane_id: 'w2:p2', project: '/workspace/docs-site', title: 'README 영문 번역', status: 'idle', runtime_binding: 'fixture-binding-idle' }, idle), messages: idle },
    { session: withPreview({ ...base, id: 'claude:fixture-unverified', pane_id: 'w3:p1', project: '/workspace/log-agent', title: '로그 수집기 설정 정리', status: 'working', runtime_binding: undefined, terminal: false, lifecycle: 'unverified' }, unverified), messages: unverified },
    { session: { ...base, id: 'pane:w3:p2', pane_id: 'w3:p2', project: '/workspace/scratch', title: '', status: 'idle', runtime_binding: undefined, chat: false, terminal: false, lifecycle: 'unbound' }, messages: [] },
    { session: { ...base, id: 'claude:fixture-unknown', pane_id: 'w4:p1', project: '/workspace/misc', title: '상태를 알 수 없는 agent', status: 'error', runtime_binding: 'fixture-binding-unknown' }, messages: [] },
  ]
}

const startCandidates: StartCandidate[] = [
  { id: 'fixture-candidate-open', name: 'sample-api', root: 'workspace', open: true, workspace: 'sample-api', placement: 'new_tab' },
  { id: 'fixture-candidate-docs', name: 'docs-site', root: 'workspace', open: false, placement: 'new_workspace' },
  { id: 'fixture-candidate-infra', name: 'infra-notes', root: 'workspace', open: false, placement: 'new_workspace' },
]
const created = { workspace_id: 'w9', tab_id: 'w9:t1', pane_id: 'w9:p1', agent: 'mobile-fixture' }

export function scenario(name: ScenarioName): Scenario {
  const out: Scenario = { name, sessions: sessions(), conditionalInput: 'supported', command: { status: 'accepted' }, latency: 60, listFails: false, socketFails: false, liveEvery: 0,
    bridge: { revision: '3c7a1e9f0b2d4865a7c9e1f3b5d7a9c1e3f5a7b9', modified: false, client_build: '0f1e2d3c4b5a6978' },
    start: { capability: { kinds: ['claude'], new_workspace: true }, candidates: startCandidates, result: { status: 'accepted', created }, latency: 2500 },
    changes: fixtureChanges(name) }
  switch (name) {
    case 'long': {
      const long = longConversation(400)
      out.sessions[1] = { session: withPreview(out.sessions[1].session, long), messages: long }
      break
    }
    case 'live': out.liveEvery = 4000; break
    case 'disconnected': out.socketFails = true; break
    case 'delivery-unknown': out.command = { status: 'delivery_unknown' }; break
    case 'rejected': out.command = { status: 'rejected', code: 'SESSION_CHANGED' }; break
    case 'empty': out.sessions = []; break
    case 'herdr-down': out.listFails = true; break
    case 'unsupported': {
      out.conditionalInput = 'unsupported'
      out.command = { status: 'rejected', code: 'HERDR_UNSUPPORTED' }
      out.sessions = out.sessions.map(({ session, messages }) => ({ session: { ...session, runtime_binding: undefined, terminal: false, lifecycle: session.id.startsWith('pane:') ? 'unbound' : 'unverified' }, messages }))
      break
    }
    case 'slow': out.latency = 2500; break
    case 'superseded': {
      const pane: Session = { ...base, id: 'pane:w5:p1', pane_id: 'w5:p1', project: '/workspace/new-agent', title: '새로 시작한 agent', status: 'working', runtime_binding: 'fixture-binding-pane', chat: false }
      const next = [message('user', '방금 시작한 작업을 이어서 정리해 줘.', 2 * minute), message('assistant', '이어서 정리하고 있습니다.', minute)]
      out.sessions.push({ session: pane, messages: [] })
      out.supersede = { from: pane.id, to: { session: withPreview({ ...pane, id: 'claude:fixture-successor', runtime_binding: 'fixture-binding-successor', chat: true }, next), messages: next }, after: 4000 }
      break
    }
    case 'start-trust': out.start.result = { status: 'accepted', code: 'AGENT_NOT_READY', created }; break
    case 'start-unknown': out.start.result = { status: 'delivery_unknown', code: 'DELIVERY_UNKNOWN' }; break
    case 'no-root':
      out.start.capability = { kinds: ['claude'], new_workspace: false }
      out.start.candidates = startCandidates.filter(item => item.open)
      break
    case 'codex': {
      const codex = [message('user', '테스트가 왜 실패하는지 찾아 줘.', 9 * minute), message('assistant', '원인을 찾았습니다. fixture의 날짜가 timezone에 따라 하루 밀립니다.', 7 * minute)]
      out.sessions.push({ session: withPreview({ ...base, agent: 'codex', id: 'codex:fixture-codex', pane_id: 'w6:p1', project: '/workspace/sample-cli', title: 'flaky test 원인 찾기', status: 'idle', runtime_binding: undefined, terminal: false, lifecycle: 'unbound' }, codex), messages: codex })
      break
    }
    case 'tools': {
      const { messages, running } = toolConversation()
      const id = out.sessions[1].session.id
      out.sessions[1] = { session: withPreview(out.sessions[1].session, messages), messages }
      const done: Message = { ...running, tool: { ...running.tool!, state: 'completed', result: ' Test Files  3 passed (3)\n      Tests  18 passed (18)' } }
      const next = tool('Read', 'CHANGELOG.md', 'running', { file_path: '/workspace/sample-api/CHANGELOG.md' }, '', 0)
      const read: Message = { ...next, tool: { ...next.tool!, state: 'completed', result: '    1→# Changelog\n    2→\n    3→## Unreleased' } }
      out.script = { session: id, steps: [
        { after: 3000, type: 'message.updated', message: done },
        { after: 4000, type: 'message.tool', message: next },
        { after: 5500, type: 'message.updated', message: read },
        { after: 6500, type: 'message.assistant', message: message('assistant', 'test 18개가 모두 통과했습니다. 같은 event를 동시에 받아도 한 번만 처리됩니다.', 0), status: 'completed' },
      ] }
      break
    }
    case 'stale-client': out.bridge = { ...out.bridge, client_build: 'a9b8c7d6e5f40312' }; break
    case 'changes-large':
    case 'default': break
  }
  return out
}

export function scenarioFromSearch(search: string): Scenario {
  const name = new URLSearchParams(search).get('scenario')
  return scenario(scenarioNames.find(item => item === name) ?? 'default')
}

export const fixtureTerminalFrame = { text: terminalFrame, cols: 66, rows: 12 }
export const fixtureReply = '요청한 test를 추가하고 전체를 실행했습니다. **42개 모두 통과**했습니다.\n\n```text\n Test Files  8 passed (8)\n      Tests  42 passed (42)\n```'
export const fixtureLiveMessage = (index: number) => `진행 상황 ${index}: \`src/charts\`의 다음 파일을 확인하고 있습니다.`
