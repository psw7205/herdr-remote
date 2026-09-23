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
}
export type Message = { id: string; role: 'user' | 'assistant'; text: string; timestamp: string }
export type Cursor = { epoch: string; sequence: number }
export type Snapshot = { cursor: Cursor; data: Message[] }
export type Event = { type: string; cursor: Cursor; payload: unknown; event_id: string }
export type Delivery = { status: 'accepted' | 'rejected' | 'delivery_unknown'; code?: string }

async function readJSON<T>(response: Response): Promise<T> {
  const body = await response.json()
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`)
  return body as T
}
export async function listSessions(): Promise<Session[]> {
  const body = await readJSON<{ sessions: Session[] }>(await fetch('/api/sessions', { cache: 'no-store' }))
  return body.sessions
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
