// Dev-only fake Bridge behind fixture.html. It answers the HTTP and WebSocket
// contract of internal/httpapi, so the real client code runs unchanged against
// anonymous sample data. Production builds never include this module.
import type { Cursor, Event, Message, Session } from '../api'
import { fixtureLiveMessage, fixtureReply, fixtureTerminalFrame, type Scenario } from './scenarios'

type Room = { session: Session; messages: Message[]; epoch: string; log: Event[]; sockets: Set<FakeSocket> }

const ECHO_MS = 700
const REPLY_MS = 2500
const origin = 'http://fixture.invalid'

let rooms = new Map<string, Room>()
let active: Scenario | undefined

function sequence(room: Room): number { return room.log.at(-1)?.cursor.sequence ?? 0 }
function cursor(room: Room): Cursor { return { epoch: room.epoch, sequence: sequence(room) } }
function preview(room: Room): Session {
  const last = room.messages.at(-1)
  if (!last) return room.session
  const text = last.text.replace(/\s+/g, ' ').trim()
  return { ...room.session, last_activity: last.timestamp, last_message: { role: last.role, text: text.length > 160 ? `${text.slice(0, 159)}…` : text } }
}

function publish(room: Room, type: string, payload: unknown) {
  const event: Event = { type, cursor: { epoch: room.epoch, sequence: sequence(room) + 1 }, payload, event_id: `${room.session.id}:${sequence(room) + 1}` }
  room.log.push(event)
  for (const socket of room.sockets) socket.deliver(event)
}
function appendMessage(room: Room, role: Message['role'], text: string) {
  const next: Message = { id: `${room.session.id}:m${room.messages.length + 1}`, role, text, timestamp: new Date().toISOString() }
  room.messages.push(next)
  publish(room, `message.${role}`, next)
}
function setStatus(room: Room, status: Session['status']) {
  room.session = { ...room.session, status }
  publish(room, 'agent.status', { status, lifecycle: room.session.lifecycle })
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}
const wait = (ms: number) => ms > 0 ? new Promise(resolve => setTimeout(resolve, ms)) : Promise.resolve()

async function answer(scenario: Scenario, method: string, url: URL, body: string): Promise<Response> {
  await wait(scenario.latency)
  if (scenario.listFails) throw new TypeError('fixture: Bridge unreachable')
  if (method === 'GET' && url.pathname === '/api/sessions') {
    const sessions = [...rooms.values()].filter(room => room.session.active).map(preview)
    return json({ sessions, herdr: { conditional_input: scenario.conditionalInput }, start: scenario.start.capability })
  }
  if (method === 'GET' && url.pathname === '/api/start-candidates') return json({ candidates: scenario.start.candidates, start: scenario.start.capability })
  if (method === 'POST' && url.pathname === '/api/sessions') {
    // The answer arrives after the agent readiness wait, like the Bridge's.
    await wait(scenario.start.latency)
    return json(scenario.start.result, scenario.start.result.status === 'rejected' ? 409 : 202)
  }
  const match = url.pathname.match(/^\/api\/sessions\/([^/]+)(\/terminal|\/commands)?$/)
  const room = match ? rooms.get(decodeURIComponent(match[1])) : undefined
  if (!match || !room) return json({ error: 'SESSION_NOT_FOUND' }, 404)
  if (method === 'GET' && !match[2]) return json({ session: preview(room), snapshot: { cursor: cursor(room), data: room.messages } })
  if (method === 'GET' && match[2] === '/terminal') {
    if (!room.session.terminal) return json({ error: 'SESSION_CHANGED' }, 409)
    return json({ text: fixtureTerminalFrame.text, pane_id: room.session.pane_id, cols: fixtureTerminalFrame.cols, rows: fixtureTerminalFrame.rows })
  }
  if (method === 'POST' && match[2] === '/commands') {
    const command = JSON.parse(body) as { command_type: string; payload: { text: string } }
    if (scenario.command.status !== 'accepted') return json(scenario.command)
    // A slash command runs locally: its transcript records are hidden from Chat.
    if (command.command_type === 'prompt' && command.payload.text.trimStart().startsWith('/')) return json({ status: 'accepted' })
    if (command.command_type === 'prompt') {
      setStatus(room, 'working')
      setTimeout(() => appendMessage(room, 'user', command.payload.text), ECHO_MS)
      setTimeout(() => { appendMessage(room, 'assistant', fixtureReply); setStatus(room, 'completed') }, REPLY_MS)
    } else if (command.command_type === 'interrupt' && room.session.status === 'working') {
      setTimeout(() => setStatus(room, 'idle'), ECHO_MS)
    }
    return json({ status: 'accepted' })
  }
  return json({ error: 'NOT_FOUND' }, 404)
}

type Listener = ((event: MessageEvent) => void) | null
class FakeSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3
  readyState = FakeSocket.CONNECTING
  onopen: ((event: globalThis.Event) => void) | null = null
  onmessage: Listener = null
  onclose: ((event: CloseEvent) => void) | null = null
  onerror: ((event: globalThis.Event) => void) | null = null
  private room: Room | undefined

  constructor(readonly url: string) {
    const parsed = new URL(url, origin)
    const id = decodeURIComponent(parsed.pathname.match(/^\/api\/sessions\/([^/]+)\/events$/)?.[1] ?? '')
    const room = rooms.get(id)
    setTimeout(() => {
      if (!room || active?.socketFails) { this.finish(); return }
      this.room = room
      room.sockets.add(this)
      this.readyState = FakeSocket.OPEN
      this.onopen?.(new globalThis.Event('open'))
      const epoch = parsed.searchParams.get('epoch')
      const after = Number(parsed.searchParams.get('sequence'))
      if (epoch !== room.epoch || after > sequence(room)) this.deliver({ type: 'session.snapshot', cursor: cursor(room), payload: { cursor: cursor(room), data: room.messages }, event_id: `${id}:snapshot` })
      else for (const event of room.log) if (event.cursor.sequence > after) this.deliver(event)
    }, 30)
  }
  deliver(event: Event) {
    if (this.readyState === FakeSocket.OPEN) this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(event) }))
  }
  send() {}
  close() { setTimeout(() => this.finish(), 0) }
  private finish() {
    if (this.readyState === FakeSocket.CLOSED) return
    this.readyState = FakeSocket.CLOSED
    this.room?.sockets.delete(this)
    this.onclose?.(new CloseEvent('close'))
  }
}

// installFixtureBackend replaces fetch and WebSocket for /api and returns a
// function that restores the originals.
export function installFixtureBackend(scenario: Scenario): () => void {
  active = scenario
  rooms = new Map(scenario.sessions.map(({ session, messages }) => [session.id, { session, messages: [...messages], epoch: `fixture-${scenario.name}`, log: [], sockets: new Set() }]))
  const realFetch = globalThis.fetch
  const realSocket = globalThis.WebSocket
  const fakeFetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : String(input), origin)
    if (!url.pathname.startsWith('/api/')) return realFetch(input, init)
    return answer(scenario, init?.method ?? 'GET', url, typeof init?.body === 'string' ? init.body : '')
  }
  const supersede = scenario.supersede
  const handover = supersede && setTimeout(() => {
    const from = rooms.get(supersede.from)
    if (!from) return
    from.session = { ...from.session, active: false, lifecycle: 'superseded', successor_id: supersede.to.session.id }
    rooms.set(supersede.to.session.id, { session: supersede.to.session, messages: [...supersede.to.messages], epoch: `fixture-${scenario.name}`, log: [], sockets: new Set() })
  }, supersede.after)
  let live: ReturnType<typeof setInterval> | undefined
  if (scenario.liveEvery > 0) {
    let index = 0
    live = setInterval(() => {
      const room = [...rooms.values()].find(item => item.session.status === 'working' && item.session.chat)
      if (room) appendMessage(room, 'assistant', fixtureLiveMessage(++index))
    }, scenario.liveEvery)
  }
  Object.defineProperty(globalThis, 'fetch', { value: fakeFetch, configurable: true, writable: true })
  Object.defineProperty(globalThis, 'WebSocket', { value: FakeSocket, configurable: true, writable: true })
  return () => {
    if (live) clearInterval(live)
    if (handover) clearTimeout(handover)
    Object.defineProperty(globalThis, 'fetch', { value: realFetch, configurable: true, writable: true })
    Object.defineProperty(globalThis, 'WebSocket', { value: realSocket, configurable: true, writable: true })
  }
}
