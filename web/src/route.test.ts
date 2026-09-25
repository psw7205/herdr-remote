import { describe, expect, it } from 'vitest'
import type { Session } from './api'
import { backAction, followAlias, formatRoute, parseRoute, resolveTerminal, type Route } from './route'

const list: Route = { session: null, view: 'chat' }
const chat: Route = { session: 'claude:native-a', view: 'chat' }
const terminal: Route = { session: 'claude:native-a', view: 'terminal' }

describe('hash route', () => {
  it('reads the list, a chat and a terminal view', () => {
    expect(parseRoute('')).toEqual(list)
    expect(parseRoute('#')).toEqual(list)
    expect(parseRoute('#session=claude%3Anative-a')).toEqual(chat)
    expect(parseRoute('#session=claude%3Anative-a&view=terminal')).toEqual(terminal)
  })
  it('round-trips ids that need encoding', () => {
    const odd: Route = { session: 'pane:w1:p2&x=1 #y', view: 'terminal' }
    expect(parseRoute(formatRoute(odd))).toEqual(odd)
    expect(formatRoute(list)).toBe('')
  })
  it('falls back to the list for a malformed id and to chat for an unknown view', () => {
    expect(parseRoute('#session=%E0%A4%A')).toEqual(list)
    expect(parseRoute('#session=claude%3Anative-a&view=changes')).toEqual(chat)
    expect(parseRoute('#view=terminal')).toEqual(list)
  })
})

describe('superseded ids in history', () => {
  it('sends an older history entry of a superseded pane item to its successor', () => {
    const aliases = new Map([['pane:w1:p2', 'claude:native-a']])
    expect(followAlias({ session: 'pane:w1:p2', view: 'chat' }, aliases)).toEqual(chat)
    expect(followAlias(terminal, aliases)).toEqual(terminal)
    expect(followAlias(list, aliases)).toEqual(list)
  })
  it('follows a chain of successors and stops on a cycle', () => {
    expect(followAlias({ session: 'a', view: 'chat' }, new Map([['a', 'b'], ['b', 'c']]))).toEqual({ session: 'c', view: 'chat' })
    expect(followAlias({ session: 'a', view: 'chat' }, new Map([['a', 'b'], ['b', 'a']])).session).toMatch(/^[ab]$/)
  })
})

describe('terminal target', () => {
  const session = { id: 'claude:native-a', terminal: true, runtime_binding: 'first' } as Session
  it('keeps the binding the Terminal opened with', () => {
    expect(resolveTerminal(terminal, session, { ...session, runtime_binding: 'second' })).toBe(session)
  })
  it('captures the listed binding when the Terminal route is entered without one', () => {
    const listed = { ...session }
    expect(resolveTerminal(terminal, null, listed)).toBe(listed)
    expect(resolveTerminal(terminal, { ...session, id: 'claude:other' }, listed)).toBe(listed)
  })
  it('has no target outside the Terminal view or for a session without one', () => {
    expect(resolveTerminal(chat, session, session)).toBeNull()
    expect(resolveTerminal(terminal, null, { ...session, terminal: false })).toBeNull()
    expect(resolveTerminal(terminal, null, undefined)).toBeNull()
  })
})

describe('back action', () => {
  it('pops the history entry the app pushed', () => {
    expect(backAction(terminal, 2)).toEqual({ kind: 'history' })
    expect(backAction(chat, 1)).toEqual({ kind: 'history' })
  })
  it('replaces with the parent view on a deep link that has no app entry to pop', () => {
    expect(backAction(terminal, 0)).toEqual({ kind: 'replace', route: chat })
    expect(backAction(chat, 0)).toEqual({ kind: 'replace', route: list })
  })
})
