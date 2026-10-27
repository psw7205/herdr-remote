import { describe, expect, it } from 'vitest'
import { chatSelection } from './chatSelection'

const inside = { id: 'message' } as unknown as Node
const outside = { id: 'composer' } as unknown as Node
const root = { contains: (node: Node | null) => node === inside }
const selection = (text: string, node: Node, collapsed = false) => ({
  isCollapsed: collapsed, rangeCount: 1, toString: () => text, getRangeAt: () => ({ commonAncestorContainer: node }),
})

describe('chat selection', () => {
  it('returns text selected inside the conversation as it was selected', () => {
    expect(chatSelection(selection('원인을 찾았습니다.\n\n- 첫째', inside), root)).toBe('원인을 찾았습니다.\n\n- 첫째')
  })
  it('ignores a caret, blank text and selections that reach outside the conversation', () => {
    expect(chatSelection(selection('', inside, true), root)).toBeNull()
    expect(chatSelection(selection(' \n ', inside), root)).toBeNull()
    expect(chatSelection(selection('draft', outside), root)).toBeNull()
    expect(chatSelection({ ...selection('x', inside), rangeCount: 0 }, root)).toBeNull()
    expect(chatSelection(null, root)).toBeNull()
    expect(chatSelection(selection('x', inside), null)).toBeNull()
  })
})
