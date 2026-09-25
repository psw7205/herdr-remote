import { afterEach, expect, it, vi } from 'vitest'
import { getSession, listSessions, sendCommand, terminalFrame, type Event } from '../api'
import { installFixtureBackend } from './backend'
import { scenario } from './scenarios'

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

it('fails the list like an unreachable Bridge', async () => {
  restore = installFixtureBackend(scenario('herdr-down'))
  await expect(listSessions()).rejects.toThrow()
})
