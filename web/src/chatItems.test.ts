import { describe, expect, it } from 'vitest'
import type { Message } from './api'
import { chatEntries, turnSummaries } from './chatItems'

const item = (id: string, role: Message['role'], timestamp: string): Message => role === 'tool'
  ? { id, role, text: '', timestamp, tool: { id: `toolu_${id}`, name: 'Bash', summary: 'echo', state: 'completed', input: '{}', input_truncated: false, result: 'ok', result_truncated: false } }
  : { id, role, text: id, timestamp }
const summaries = (messages: Message[], live: boolean) => Object.fromEntries([...turnSummaries(chatEntries(messages), live)].map(([key, { duration, answer }]) => [key, { duration, answer: answer?.id }]))

describe('turn summaries', () => {
  it('spans a finished turn from the prompt to the last agent item, tool or text', () => {
    const messages = [
      item('u1', 'user', '2026-10-27T10:00:00Z'),
      item('a1', 'assistant', '2026-10-27T10:00:05Z'),
      item('t1', 'tool', '2026-10-27T10:01:00Z'),
      item('a2', 'assistant', '2026-10-27T10:02:36Z'),
      item('u2', 'user', '2026-10-27T10:05:00Z'),
      item('a3', 'assistant', '2026-10-27T10:05:10Z'),
      item('t2', 'tool', '2026-10-27T10:05:40Z'),
    ]
    expect(summaries(messages, false)).toEqual({ a2: { duration: 156_000, answer: 'a2' }, t2: { duration: 40_000, answer: 'a3' } })
  })
  it('leaves the last turn open while the agent is live', () => {
    const messages = [
      item('u1', 'user', '2026-10-27T10:00:00Z'),
      item('a1', 'assistant', '2026-10-27T10:00:30Z'),
      item('u2', 'user', '2026-10-27T10:01:00Z'),
      item('a2', 'assistant', '2026-10-27T10:01:20Z'),
    ]
    expect(summaries(messages, true)).toEqual({ a1: { duration: 30_000, answer: 'a1' } })
  })
  it('gives no duration to a prompt without a reply, a turn without a prompt or an unreadable time', () => {
    expect(summaries([item('u1', 'user', '2026-10-27T10:00:00Z')], false)).toEqual({})
    expect(summaries([item('a0', 'assistant', '2026-10-27T09:59:00Z'), item('u1', 'user', '2026-10-27T10:00:00Z')], false)).toEqual({ a0: { answer: 'a0' } })
    expect(summaries([item('u1', 'user', 'unknown'), item('a1', 'assistant', '2026-10-27T10:00:30Z')], false)).toEqual({ a1: { answer: 'a1' } })
    expect(summaries([item('u1', 'user', '2026-10-27T10:00:30Z'), item('a1', 'assistant', '2026-10-27T10:00:00Z')], false)).toEqual({ a1: { answer: 'a1' } })
  })
  it('offers no answer for a turn of tool items only', () => {
    expect(summaries([item('u1', 'user', '2026-10-27T10:00:00Z'), item('t1', 'tool', '2026-10-27T10:00:09Z')], false)).toEqual({ t1: { duration: 9_000 } })
  })
})
