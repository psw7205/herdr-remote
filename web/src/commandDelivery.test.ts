import { afterEach, expect, it, vi } from 'vitest'
import { chooseCommand, readPending, type PendingCommand } from './commandDelivery'

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
