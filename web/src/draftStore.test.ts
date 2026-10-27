import { describe, expect, it } from 'vitest'
import { DRAFT_LIMIT, appendQuote, appendQuoteToDraft, readDraft, writeDraft } from './draftStore'

class MemoryStorage {
  private items = new Map<string, string>()
  get length() { return this.items.size }
  key(index: number) { return [...this.items.keys()][index] ?? null }
  getItem(key: string) { return this.items.get(key) ?? null }
  setItem(key: string, value: string) { this.items.set(key, value) }
  removeItem(key: string) { this.items.delete(key) }
  keys() { return [...this.items.keys()] }
}

const throwing = {
  get length(): number { throw new Error('denied') },
  key(): string | null { throw new Error('denied') },
  getItem(): string | null { throw new Error('denied') },
  setItem() { throw new Error('denied') },
  removeItem() { throw new Error('denied') },
}

describe('draft store', () => {
  it('reads back the text it wrote with when it was written', () => {
    const storage = new MemoryStorage()
    writeDraft('claude:a', '두 줄\n초안', storage)
    expect(readDraft('claude:a', storage)).toBe('두 줄\n초안')
    expect(readDraft('claude:b', storage)).toBe('')
    const stored: unknown = JSON.parse(storage.getItem('draft:claude:a') ?? 'null')
    expect(stored).toEqual({ text: '두 줄\n초안', updated: expect.any(Number) })
  })

  it('removes the key instead of keeping an empty draft', () => {
    const storage = new MemoryStorage()
    writeDraft('claude:a', 'hello', storage)
    writeDraft('claude:a', '', storage)
    expect(storage.getItem('draft:claude:a')).toBeNull()
    expect(storage.length).toBe(0)
  })

  it(`keeps at most ${DRAFT_LIMIT} drafts by dropping the least recently written`, () => {
    const storage = new MemoryStorage()
    const order = [7, 3, 12, 1, 18, 5, 20, 9, 14, 2, 16, 11, 4, 19, 8, 13, 6, 17, 10, 15]
    for (const updated of order) storage.setItem(`draft:s${updated}`, JSON.stringify({ text: `t${updated}`, updated }))
    writeDraft('fresh', 'new', storage)
    const drafts = storage.keys().filter(key => key.startsWith('draft:'))
    expect(drafts).toHaveLength(DRAFT_LIMIT)
    expect(drafts).toContain('draft:fresh')
    expect(drafts).not.toContain('draft:s1')
    expect(drafts).toContain('draft:s2')
  })

  it('evicts nothing while under the limit', () => {
    const storage = new MemoryStorage()
    for (let updated = 1; updated <= 10; updated++) storage.setItem(`draft:s${updated}`, JSON.stringify({ text: 't', updated }))
    writeDraft('fresh', 'new', storage)
    expect(storage.length).toBe(11)
  })

  it('evicts unreadable drafts first and never touches other keys', () => {
    const storage = new MemoryStorage()
    storage.setItem('command:s0', '{"id":"c"}')
    storage.setItem('terminal:font', '14')
    storage.setItem('draft:broken', 'not json')
    for (let updated = 1; updated < DRAFT_LIMIT; updated++) storage.setItem(`draft:s${updated}`, JSON.stringify({ text: 't', updated }))
    writeDraft('fresh', 'new', storage)
    expect(storage.getItem('draft:broken')).toBeNull()
    expect(storage.getItem('draft:s1')).not.toBeNull()
    expect(storage.getItem('command:s0')).toBe('{"id":"c"}')
    expect(storage.getItem('terminal:font')).toBe('14')
  })

  it('reads a corrupt or foreign value as an empty draft', () => {
    const storage = new MemoryStorage()
    for (const value of ['not json', '"plain"', '42', 'null', '{}', '{"text":3,"updated":1}', '{"text":"x"}']) {
      storage.setItem('draft:a', value)
      expect(readDraft('a', storage)).toBe('')
    }
  })

  it('treats storage that throws as empty and writes nothing', () => {
    expect(readDraft('a', throwing)).toBe('')
    expect(() => writeDraft('a', 'text', throwing)).not.toThrow()
    expect(() => writeDraft('a', '', throwing)).not.toThrow()
  })

  it('falls back silently when the platform has no localStorage', () => {
    expect(() => writeDraft('a', 'text')).not.toThrow()
    expect(readDraft('a')).toBe('')
  })
})

describe('quote', () => {
  it('quotes each line and leaves the cursor on a fresh line', () => {
    expect(appendQuote('', '첫 줄\n\n셋째 줄\n')).toBe('> 첫 줄\n>\n> 셋째 줄\n\n')
  })

  it('separates the quote from existing text by one blank line', () => {
    expect(appendQuote('이전 초안\n', 'a')).toBe('이전 초안\n\n> a\n\n')
  })

  it('ignores a selection with no text', () => {
    expect(appendQuote('초안', ' \n\t')).toBe('초안')
  })

  it('appends to the stored draft of a session that is not open', () => {
    const storage = new MemoryStorage()
    writeDraft('claude:a', '초안', storage)
    appendQuoteToDraft('claude:a', 'terminal line', storage)
    expect(readDraft('claude:a', storage)).toBe('초안\n\n> terminal line\n\n')
  })
})
