export type Session = {
  id: string
  agent: string
  pane_id: string
  project: string
  title: string
  status: 'needs_attention' | 'working' | 'idle' | 'completed' | 'error'
  runtime_binding?: string
  chat: boolean
  terminal: boolean
  active: boolean
  lifecycle?: Lifecycle
  successor_id?: string
  // Newest visible Chat message, as a native transcript timestamp and a short
  // preview. Absent when the session has no visible message or no Chat.
  last_activity?: string
  last_message?: { role: 'user' | 'assistant'; text: string }
}
// unverified: a verified claude: session lost its runtime binding while Herdr
// still runs the agent, so every input and Terminal stays closed until it is
// verified again. unbound: the item never had a binding: a pane: item whose
// native session was never identified, or a read-only codex: session.
// superseded: the pane: item continues as successor_id, which carries its own
// binding if it has one.
export type Lifecycle = 'active' | 'unverified' | 'unbound' | 'superseded' | 'ended'
// A tool item (role 'tool') carries tool and an empty text. Input and result
// are capped by the Bridge; the truncated flags say so. A result-less call is
// 'running' until the transcript moves on, then 'unknown'.
export type ToolActivity = { id: string; name: string; summary: string; state: 'running' | 'completed' | 'error' | 'unknown'; input: string; input_truncated: boolean; result: string; result_truncated: boolean }
export type Message = { id: string; role: 'user' | 'assistant' | 'tool'; text: string; timestamp: string; tool?: ToolActivity }
export type Cursor = { epoch: string; sequence: number }
export type Snapshot = { cursor: Cursor; data: Message[] }
export type Event = { type: string; cursor: Cursor; payload: unknown; event_id: string }
export type Delivery = { status: 'accepted' | 'rejected' | 'delivery_unknown'; code?: string }
export type ConditionalInput = 'supported' | 'unsupported' | 'unknown'
// Which agent kinds the Bridge may ask Herdr to start, and whether a new
// workspace is possible (only with a -project-root). ADR-037.
export type StartCapability = { kinds: string[]; new_workspace: boolean }
export const noStart: StartCapability = { kinds: [], new_workspace: false }
export type BridgeBuild = { revision: string; modified: boolean; clientBuild: string }
export type SessionList = { sessions: Session[]; conditionalInput: ConditionalInput; start: StartCapability; bridge: BridgeBuild | null }
export type Placement = 'new_workspace' | 'new_tab'
// A folder a new session may start in. The Bridge resolves the opaque id to
// a path; the client never sends one.
export type StartCandidate = { id: string; name: string; root?: string; open: boolean; workspace?: string; placement: Placement }
export type StartDelivery = Delivery & { created?: { workspace_id: string; tab_id: string; pane_id: string; agent: string } }

async function readJSON<T>(response: Response): Promise<T> {
  const body = await response.json()
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`)
  return body as T
}
export async function listSessions(): Promise<SessionList> {
  const body = await readJSON<{ sessions: Session[]; herdr?: { conditional_input?: ConditionalInput }; start?: StartCapability; bridge?: { revision: string; modified: boolean; client_build: string } }>(await fetch('/api/sessions', { cache: 'no-store' }))
  const bridge = body.bridge ? { revision: body.bridge.revision, modified: body.bridge.modified, clientBuild: body.bridge.client_build } : null
  return { sessions: body.sessions, conditionalInput: body.herdr?.conditional_input ?? 'unknown', start: body.start ?? noStart, bridge }
}
export async function listStartCandidates(): Promise<{ candidates: StartCandidate[]; start: StartCapability }> {
  return readJSON(await fetch('/api/start-candidates', { cache: 'no-store' }))
}
// A start is answered after Herdr's agent readiness wait (up to 30s), so the
// caller's timeout must be longer. The body is a receipt result either way.
export async function startSession(candidate: string, kind: string, placement: Placement, commandID: string, signal?: AbortSignal): Promise<StartDelivery> {
  const response = await fetch('/api/sessions', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, signal,
    body: JSON.stringify({ command_id: commandID, command_type: 'session_start', payload: { candidate_id: candidate, kind, placement } }),
  })
  const body = await response.json()
  if (typeof body?.status === 'string') return body as StartDelivery
  // A validation or Origin error is answered before any receipt: nothing ran.
  if (typeof body?.error === 'string') return { status: 'rejected', code: body.error }
  throw new Error(`HTTP ${response.status}`)
}
export function sessionLifecycle(session: Pick<Session, 'active' | 'lifecycle'>): Lifecycle {
  return session.lifecycle ?? (session.active ? 'active' : 'ended')
}
export function lifecycleNotice(session: Pick<Session, 'active' | 'lifecycle'>): string | null {
  return sessionLifecycle(session) === 'unverified' ? 'agent는 Herdr에서 계속 실행 중이지만 이 대화와의 연결을 확인할 수 없습니다. 연결이 확인될 때까지 입력과 Terminal을 사용할 수 없습니다.' : null
}
export function composerHint(session: Pick<Session, 'active' | 'lifecycle' | 'terminal' | 'status' | 'chat'> & Partial<Pick<Session, 'agent'>>): string {
  const lifecycle = sessionLifecycle(session)
  if (lifecycle === 'ended') return '종료된 세션에는 입력할 수 없습니다.'
  if (lifecycle === 'unverified') return 'agent 연결을 확인할 수 없어 입력할 수 없습니다.'
  if (lifecycle === 'superseded') return '같은 agent의 대화로 전환하는 중입니다.'
  if (lifecycle === 'unbound' && session.agent === 'codex') return 'Codex 입력은 아직 지원하지 않습니다. PC의 Herdr에서 입력하세요.'
  if (lifecycle === 'unbound') return '이 세션은 원격 입력을 지원하지 않습니다. PC의 Herdr에서 입력하세요.'
  const place = session.terminal ? 'Terminal' : 'PC의 Herdr'
  if (!session.chat) return `이 세션은 ${place}에서 입력하세요.`
  if (session.status === 'working') return '작업 중입니다. 끝나면 이어서 입력할 수 있습니다.'
  if (session.status === 'needs_attention') return `입력을 기다리고 있습니다. ${place}에서 응답하세요.`
  return `지금은 ${place}에서 입력하세요.`
}
export function successorOf(session: Pick<Session, 'lifecycle' | 'successor_id'>): string | null {
  return session.lifecycle === 'superseded' && session.successor_id ? session.successor_id : null
}
// supersededBy returns the listed successor of an open session that left the
// list because it was superseded, or null. The successor's own
// runtime_binding comes from the list, never from the superseded item.
// An ended item is never superseded (it can only revive, which lists it
// again), so its id is kept in ended and not fetched until it is listed again.
export async function supersededBy(id: string | null, sessions: Session[], ended: Set<string> = new Set()): Promise<Session | null> {
  if (!id) return null
  if (sessions.some(item => item.id === id)) { ended.delete(id); return null }
  if (ended.has(id)) return null
  const data = await getSession(id).catch(() => null)
  if (!data) return null
  if (sessionLifecycle(data.session) === 'ended') { ended.add(id); return null }
  const next = successorOf(data.session)
  return next ? sessions.find(item => item.id === next) ?? null : null
}
export async function getSession(id: string): Promise<{ session: Session; snapshot: Snapshot }> {
  return readJSON(await fetch(`/api/sessions/${encodeURIComponent(id)}`, { cache: 'no-store' }))
}
export async function sendCommand(session: Session, kind: 'prompt' | 'interrupt' | 'terminal_input', text: string, commandID: string): Promise<Delivery> {
  const response = await fetch(`/api/sessions/${encodeURIComponent(session.id)}/commands`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ command_id: commandID, session_id: session.id, runtime_binding: session.runtime_binding, command_type: kind, payload: { text } }),
  })
  return response.json() as Promise<Delivery>
}
export async function terminalFrame(session: Session, signal?: AbortSignal): Promise<{ text: string; pane_id: string; cols?: number; rows?: number }> {
  return readJSON(await fetch(`/api/sessions/${encodeURIComponent(session.id)}/terminal`, {
    headers: { 'X-Runtime-Binding': session.runtime_binding ?? '' }, cache: 'no-store', signal,
  }))
}
// Changed Files (ADR-016): the Bridge reads the item's project with git and
// compares it with HEAD. Counts are absent when unknown (binary content, a
// symlink, or a listing cut short).
export type ChangeStatus = 'modified' | 'added' | 'deleted' | 'renamed'
export type ChangedFile = { path: string; old_path?: string; status: ChangeStatus; untracked?: boolean; binary?: boolean; additions: number | null; deletions: number | null }
export type Changes = { repository: boolean; branch?: string; initial?: boolean; files: ChangedFile[]; total: number; truncated: boolean }
// content 'none' is an entry the Bridge does not read, such as an untracked symlink.
export type FileDiff = ChangedFile & { content: 'text' | 'binary' | 'none'; diff: string; truncated: boolean }

export async function getChanges(id: string, signal?: AbortSignal): Promise<Changes> {
  return readJSON(await fetch(`/api/sessions/${encodeURIComponent(id)}/changes`, { cache: 'no-store', signal }))
}
// path must be one the listing returned; the Bridge rejects any other string.
export async function getFileDiff(id: string, path: string, signal?: AbortSignal): Promise<FileDiff> {
  const query = new URLSearchParams({ path })
  return readJSON(await fetch(`/api/sessions/${encodeURIComponent(id)}/changes/diff?${query}`, { cache: 'no-store', signal }))
}
export function eventURL(id: string, cursor: Cursor): string {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = new URL(`/api/sessions/${encodeURIComponent(id)}/events`, location.href)
  url.protocol = scheme
  url.searchParams.set('epoch', cursor.epoch)
  url.searchParams.set('sequence', String(cursor.sequence))
  return url.toString()
}
