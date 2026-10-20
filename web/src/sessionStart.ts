import type { NoticeTone } from './Notice'
import type { Placement, Session, SessionList, StartCandidate, StartDelivery } from './api'

// One start intent at a time. Its command ID is stored before sending so a
// reload or timeout re-checks the same receipt instead of starting again.
export type PendingStart = { id: string; candidate: string; placement: Placement; kind: string }
export const PENDING_START_KEY = 'start:pending'

export function readPendingStart(): PendingStart | null {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(PENDING_START_KEY) ?? 'null')
    if (value && typeof value === 'object' && 'id' in value && 'candidate' in value && 'placement' in value && 'kind' in value
      && typeof value.id === 'string' && typeof value.candidate === 'string' && typeof value.kind === 'string'
      && (value.placement === 'new_workspace' || value.placement === 'new_tab')) return value as PendingStart
  } catch { /* A corrupt entry cannot authorize a new start. */ }
  return null
}
export function writePendingStart(pending: PendingStart | null) {
  try {
    if (pending) sessionStorage.setItem(PENDING_START_KEY, JSON.stringify(pending))
    else sessionStorage.removeItem(PENDING_START_KEY)
  } catch { /* Storage can be unavailable; the receipt still dedupes a retry. */ }
}

// The same intent keeps its ID. Another folder while a start is uncertain is
// refused until the person has checked and cleared it.
export function chooseStart(pending: PendingStart | null, candidate: StartCandidate, kind: string, newID: () => string): PendingStart | null {
  if (pending) return pending.candidate === candidate.id && pending.placement === candidate.placement && pending.kind === kind ? pending : null
  return { id: newID(), candidate: candidate.id, placement: candidate.placement, kind }
}

// pane names the new session's pane when Herdr accepted the start: the client
// opens that session's Chat, whose own status shows whether Claude is ready.
// text is shown only when the session does not appear in the list in time.
export type StartOutcome = { tone: NoticeTone; text: string; settled: boolean; pane?: string }

export const permissionNotice = '새 세션의 권한 모드는 PC 사용자 shell 설정(alias 등)을 따릅니다. Bridge는 권한 모드를 정하거나 보장하지 않습니다.'
export const noRootNotice = '새 workspace는 Bridge를 -project-root와 함께 실행해야 만들 수 있습니다. 지금은 열린 workspace에 새 tab만 열 수 있습니다.'
export const uncertainStart: StartOutcome = { tone: 'warning', text: 'Herdr가 요청을 처리했는지 알 수 없습니다. 자동으로 다시 보내지 않습니다. 결과를 다시 확인하거나 PC의 Herdr에서 확인하세요.', settled: false }

// settled means the receipt holds a final answer, so the pending ID is done.
export function startOutcome(result: StartDelivery): StartOutcome {
  if (result.status === 'delivery_unknown') return uncertainStart
  if (result.status === 'accepted') {
    const pane = result.created?.pane_id
    if (result.code === 'AGENT_NOT_READY') return { tone: 'warning', text: 'Claude가 폴더 신뢰 같은 시작 화면에서 기다리고 있습니다. PC의 Herdr에서 확인하면 목록에서 이어서 열 수 있습니다.', settled: true, pane }
    return { tone: 'info', text: 'Claude를 시작했습니다. 목록에 나타나면 열 수 있습니다.', settled: true, pane }
  }
  switch (result.code) {
    case 'CANDIDATE_CHANGED':
      return { tone: 'danger', text: '폴더 상태가 바뀌어 시작하지 않았습니다. 목록을 새로 고친 뒤 다시 고르세요.', settled: true }
    case 'HERDR_UNAVAILABLE':
      return { tone: 'danger', text: 'Herdr에 연결할 수 없어 시작하지 않았습니다.', settled: true }
    case 'HERDR_UNSUPPORTED':
      return { tone: 'danger', text: '이 Herdr는 새 세션 생성을 지원하지 않아 시작하지 않았습니다.', settled: true }
    case 'HERDR_REJECTED':
      return { tone: 'danger', text: 'Herdr가 workspace나 tab 생성을 거부했습니다.', settled: true }
    case 'AGENT_START_FAILED':
      return { tone: 'danger', text: result.created ? 'workspace나 tab은 만들어졌지만 Claude가 시작되지 않았습니다. 만든 tab은 자동으로 닫지 않으니 PC의 Herdr에서 확인하세요.' : 'Claude가 시작되지 않았습니다. PC의 Herdr에서 확인하세요.', settled: true }
    case 'COMMAND_ID_CONFLICT':
      return { tone: 'danger', text: '이전 요청과 내용이 달라 시작하지 않았습니다.', settled: true }
    default:
      return { tone: 'danger', text: `시작이 거부됐습니다 (${result.code ?? '알 수 없음'}).`, settled: true }
  }
}

export const FIND_ATTEMPTS = 5
export const FIND_INTERVAL_MS = 3000

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise(resolve => {
    const timer = setTimeout(() => { signal?.removeEventListener('abort', done); resolve() }, ms)
    const done = () => { clearTimeout(timer); resolve() }
    signal?.addEventListener('abort', done, { once: true })
  })
}

// findStartedSession looks the new session up by its pane: its item ID can be
// pane:<pane> or claude:<native> depending on when the Bridge verified it, and
// may change between the two. The Bridge lists the pane within one of its
// own refreshes, so a few list polls are enough.
export async function findStartedSession(pane: string, list: () => Promise<SessionList>, { attempts = FIND_ATTEMPTS, interval = FIND_INTERVAL_MS, signal }: { attempts?: number; interval?: number; signal?: AbortSignal } = {}): Promise<Session | null> {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    if (attempt > 0) await sleep(interval, signal)
    if (signal?.aborted) return null
    const sessions = await list().then(body => body.sessions, () => [])
    const found = sessions.find(item => item.pane_id === pane)
    if (found) return found
  }
  return null
}

export type CandidateGroup = { key: string; title: string; candidates: StartCandidate[] }

// Open workspaces first: picking one opens a tab there instead of a second
// workspace for the same folder. Other folders follow under their root.
export function groupCandidates(candidates: StartCandidate[]): CandidateGroup[] {
  const groups: CandidateGroup[] = []
  const open = candidates.filter(item => item.open)
  if (open.length > 0) groups.push({ key: 'open', title: '열린 workspace', candidates: open })
  for (const item of candidates) {
    if (item.open) continue
    const key = `root:${item.root ?? ''}`
    let group = groups.find(entry => entry.key === key)
    if (!group) { group = { key, title: item.root ?? '폴더', candidates: [] }; groups.push(group) }
    group.candidates.push(item)
  }
  return groups
}

export function placementLabel(candidate: StartCandidate): string {
  return candidate.placement === 'new_tab' ? `${candidate.workspace || candidate.name}에 새 tab` : '새 workspace'
}
