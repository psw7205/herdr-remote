import { afterEach, expect, it, vi } from 'vitest'
import { chooseCommand, pendingCommandCopy, readPending, rejectedMessage, type PendingCommand } from './commandDelivery'

const pending: PendingCommand = { id: 'same-id', binding: 'binding-a', text: 'continue' }
it('reuses the command ID for the same prompt after a timeout or reload', () => {
  const selected = chooseCommand(pending, 'binding-a', 'continue', () => 'new-id')
  expect(selected).toEqual(pending)
})
it('blocks a changed prompt while earlier delivery remains uncertain', () => {
  expect(chooseCommand(pending, 'binding-a', 'different', () => 'new-id')).toBeNull()
  expect(chooseCommand(pending, 'binding-b', 'continue', () => 'new-id')).toBeNull()
})
it('uses a fresh ID only when no uncertain command exists', () => {
  expect(chooseCommand(null, 'binding-a', 'continue', () => 'new-id')).toEqual({ id: 'new-id', binding: 'binding-a', text: 'continue' })
})

afterEach(() => vi.unstubAllGlobals())
it('recovers the same pending command from browser storage after reload', () => {
  vi.stubGlobal('sessionStorage', { getItem: () => JSON.stringify(pending) })
  expect(readPending('command:session')).toEqual(pending)
})
it('points pending-command guidance to Terminal only when the session has one', () => {
  for (const text of Object.values(pendingCommandCopy(true))) expect(text).toContain('Terminal')
  for (const text of Object.values(pendingCommandCopy(false))) {
    expect(text).toContain('PC의 Herdr')
    expect(text).not.toContain('Terminal')
  }
})
it('explains a rejection from a Herdr build without conditional input', () => {
  expect(rejectedMessage('HERDR_UNSUPPORTED')).toContain('Herdr patch')
  expect(rejectedMessage('INPUT_REJECTED')).toBe('전달이 거부됐습니다 (INPUT_REJECTED).')
  expect(rejectedMessage('SESSION_CHANGED')).toBe('세션이 바뀌어 전달하지 않았습니다. 목록에서 다시 여세요.')
})
it('points a busy or waiting agent to Terminal instead of the session list', () => {
  const text = rejectedMessage('AGENT_NOT_READY')
  expect(text).toContain('Terminal')
  expect(text).not.toContain('목록')
})
