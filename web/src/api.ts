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
}
// unverified: a verified claude: session lost its runtime binding while Herdr
// still runs the agent, so every input and Terminal stays closed until it is
// verified again. unbound: a pane: item whose native session was never
// identified has no binding. superseded: the pane: item continues as
// successor_id, which carries its own binding.
export type Lifecycle = 'active' | 'unverified' | 'unbound' | 'superseded' | 'ended'
export type Message = { id: string; role: 'user' | 'assistant'; text: string; timestamp: string }
export type Cursor = { epoch: string; sequence: number }
export type Snapshot = { cursor: Cursor; data: Message[] }
export type Event = { type: string; cursor: Cursor; payload: unknown; event_id: string }
export type Delivery = { status: 'accepted' | 'rejected' | 'delivery_unknown'; code?: string }
export type ConditionalInput = 'supported' | 'unsupported' | 'unknown'
export type SessionList = { sessions: Session[]; conditionalInput: ConditionalInput }

async function readJSON<T>(response: Response): Promise<T> {
  const body = await response.json()
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`)
  return body as T
}
export async function listSessions(): Promise<SessionList> {
  const body = await readJSON<{ sessions: Session[]; herdr?: { conditional_input?: ConditionalInput } }>(await fetch('/api/sessions', { cache: 'no-store' }))
  return { sessions: body.sessions, conditionalInput: body.herdr?.conditional_input ?? 'unknown' }
}
export function sessionLifecycle(session: Pick<Session, 'active' | 'lifecycle'>): Lifecycle {
  return session.lifecycle ?? (session.active ? 'active' : 'ended')
}
export function lifecycleNotice(session: Pick<Session, 'active' | 'lifecycle'>): string | null {
  return sessionLifecycle(session) === 'unverified' ? 'agent는 Herdr에서 계속 실행 중이지만 이 대화와의 연결을 확인할 수 없습니다. 연결이 확인될 때까지 입력과 Terminal을 사용할 수 없습니다.' : null
}
export function composerHint(session: Pick<Session, 'active' | 'lifecycle' | 'terminal'>): string {
  const lifecycle = sessionLifecycle(session)
  if (lifecycle === 'ended') return '종료된 세션에는 입력할 수 없습니다.'
  if (lifecycle === 'unverified') return 'agent 연결을 확인할 수 없어 입력할 수 없습니다.'
  if (lifecycle === 'superseded') return '같은 agent의 대화로 전환하는 중입니다.'
  return session.terminal ? '이 상태의 입력은 Terminal에서 진행하세요.' : '이 상태의 입력은 PC의 Herdr에서 진행하세요.'
}
export function sessionCardText(session: Pick<Session, 'chat' | 'terminal' | 'lifecycle'>): string {
  if (session.lifecycle === 'unverified') return '연결 확인 불가 · 입력 사용 불가'
  // unbound, or an older Bridge that sent no lifecycle and no binding.
  if (session.lifecycle === 'unbound' || !session.terminal) return '입력·Terminal 사용 불가'
  return session.chat ? '기존 대화 연결됨' : 'Terminal에서 확인 가능'
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
export async function terminalFrame(session: Session): Promise<{ text: string; pane_id: string; cols?: number; rows?: number }> {
  return readJSON(await fetch(`/api/sessions/${encodeURIComponent(session.id)}/terminal`, {
    headers: { 'X-Runtime-Binding': session.runtime_binding ?? '' }, cache: 'no-store',
  }))
}
export function eventURL(id: string, cursor: Cursor): string {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = new URL(`/api/sessions/${encodeURIComponent(id)}/events`, location.href)
  url.protocol = scheme
  url.searchParams.set('epoch', cursor.epoch)
  url.searchParams.set('sequence', String(cursor.sequence))
  return url.toString()
}
