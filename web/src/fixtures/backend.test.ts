import { afterEach, expect, it, vi } from 'vitest'
import { getChanges, getFileDiff, getSession, listSessions, sendCommand, terminalFrame, type Event } from '../api'
import { installFixtureBackend } from './backend'
import { scenario } from './scenarios'
import { COLLAPSE_AFTER } from '../ToolActivity'

let restore: (() => void) | undefined
afterEach(() => { restore?.(); restore = undefined; vi.useRealTimers() })

function open(url: string) {
  const events: Event[] = []
  const socket = new WebSocket(url)
  socket.onmessage = message => { events.push(JSON.parse(String(message.data)) as Event) }
  return { socket, events }
}

it('serves the fixture list through the real client parser', async () => {
  restore = installFixtureBackend(scenario('default'))
  const list = await listSessions()
  expect(list.conditionalInput).toBe('supported')
  expect(new Set(list.sessions.map(item => item.status))).toEqual(new Set(['needs_attention', 'working', 'completed', 'idle', 'error']))
  expect(list.sessions.some(item => item.lifecycle === 'unverified')).toBe(true)
})

it('continues the snapshot cursor with the echoed prompt and the reply', async () => {
  vi.useFakeTimers()
  restore = installFixtureBackend({ ...scenario('default'), latency: 0 })
  const idle = (await listSessions()).sessions.find(item => item.status === 'idle')!
  const { snapshot } = await getSession(idle.id)
  const { events } = open(`ws://fixture.invalid/api/sessions/${encodeURIComponent(idle.id)}/events?epoch=${snapshot.cursor.epoch}&sequence=${snapshot.cursor.sequence}`)
  expect(await sendCommand(idle, 'prompt', '테스트 다시 실행', 'command-1')).toEqual({ status: 'accepted' })
  await vi.advanceTimersByTimeAsync(10_000)
  const messages = events.filter(event => event.type.startsWith('message.'))
  expect(messages.map(event => event.type)).toEqual(['message.user', 'message.assistant'])
  expect(events.map(event => event.cursor.sequence)).toEqual(events.map((_, index) => snapshot.cursor.sequence + index + 1))
  expect(events.every(event => event.cursor.epoch === snapshot.cursor.epoch)).toBe(true)
})

it('replays a fresh snapshot for a cursor from another epoch', async () => {
  vi.useFakeTimers()
  restore = installFixtureBackend({ ...scenario('default'), latency: 0 })
  const [first] = (await listSessions()).sessions
  const { events } = open(`ws://fixture.invalid/api/sessions/${encodeURIComponent(first.id)}/events?epoch=stale&sequence=0`)
  await vi.advanceTimersByTimeAsync(100)
  expect(events[0]?.type).toBe('session.snapshot')
})

it('answers uncertain delivery and Terminal frames per scenario', async () => {
  restore = installFixtureBackend(scenario('delivery-unknown'))
  const [first] = (await listSessions()).sessions.filter(item => item.terminal)
  expect((await sendCommand(first, 'prompt', 'x', 'command-2')).status).toBe('delivery_unknown')
  const frame = await terminalFrame(first)
  expect(frame.text.length).toBeGreaterThan(0)
  expect(frame.cols).toBeGreaterThan(0)
})

it('serves Changed Files and only diffs a listed path', async () => {
  restore = installFixtureBackend({ ...scenario('default'), latency: 0 })
  const changes = await getChanges('claude:fixture-working')
  expect(changes.repository).toBe(true)
  expect(new Set(changes.files.map(item => item.status))).toEqual(new Set(['modified', 'added', 'deleted', 'renamed']))
  for (const item of changes.files) expect((await getFileDiff('claude:fixture-working', item.path)).path).toBe(item.path)
  await expect(getFileDiff('claude:fixture-working', './src/charts/LineChart.tsx')).rejects.toThrow('FILE_NOT_CHANGED')
  expect((await getChanges('pane:w3:p2')).repository).toBe(false)
  expect((await getChanges('claude:fixture-done')).files).toEqual([])
  await expect(getChanges('claude:fixture-unknown')).rejects.toThrow('GIT_TIMEOUT')
})

it('marks the large listing and diff as truncated', async () => {
  restore = installFixtureBackend({ ...scenario('changes-large'), latency: 0 })
  const changes = await getChanges('claude:fixture-working')
  expect(changes.truncated).toBe(true)
  expect(changes.total).toBeGreaterThan(changes.files.length)
  expect((await getFileDiff('claude:fixture-working', changes.files[0].path)).truncated).toBe(true)
})

it('fails the list like an unreachable Bridge', async () => {
  restore = installFixtureBackend(scenario('herdr-down'))
  await expect(listSessions()).rejects.toThrow()
})

it('updates a running tool in place on the same cursor and replays it', async () => {
  vi.useFakeTimers()
  restore = installFixtureBackend({ ...scenario('tools'), latency: 0 })
  const working = (await listSessions()).sessions.find(item => item.status === 'working')!
  expect(working.last_message?.role).toBe('assistant')
  const { snapshot } = await getSession(working.id)
  const running = snapshot.data.find(item => item.role === 'tool' && item.tool?.state === 'running')!
  expect(snapshot.data.filter(item => item.role === 'tool').length).toBeGreaterThan(COLLAPSE_AFTER)
  await vi.advanceTimersByTimeAsync(10_000)
  const { events } = open(`ws://fixture.invalid/api/sessions/${encodeURIComponent(working.id)}/events?epoch=${snapshot.cursor.epoch}&sequence=${snapshot.cursor.sequence}`)
  await vi.advanceTimersByTimeAsync(100)
  expect(events.map(event => event.type)).toEqual(['message.updated', 'message.tool', 'message.updated', 'message.assistant', 'agent.status'])
  expect(events[0].payload).toMatchObject({ id: running.id, tool: { state: 'completed' } })
  expect((await getSession(working.id)).snapshot.data.find(item => item.id === running.id)?.tool?.state).toBe('completed')
})
