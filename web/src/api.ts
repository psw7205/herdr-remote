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
}
// unverified: Herdr still runs the agent on the pane, but its runtime binding
// cannot be verified, so every input and Terminal stays closed.
export type Lifecycle = 'active' | 'unverified' | 'ended'
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
  return session.terminal ? '이 상태의 입력은 Terminal에서 진행하세요.' : '이 상태의 입력은 PC의 Herdr에서 진행하세요.'
}
export function sessionCardText(session: Pick<Session, 'chat' | 'terminal' | 'lifecycle'>): string {
  if (session.lifecycle === 'unverified') return '연결 확인 불가 · 입력 사용 불가'
  if (session.chat) return '기존 대화 연결됨'
  return session.terminal ? 'Terminal에서 확인 가능' : '입력·Terminal 사용 불가'
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
export async function terminalFrame(session: Session): Promise<{ text: string; pane_id: string }> {
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
