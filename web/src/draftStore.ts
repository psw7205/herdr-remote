// Drafts live in localStorage because iOS drops sessionStorage when it kills
// an installed PWA. Each value is JSON { text, updated }; updated orders eviction.
export type DraftStorage = Pick<Storage, 'length' | 'key' | 'getItem' | 'setItem' | 'removeItem'>

const PREFIX = 'draft:'
export const DRAFT_LIMIT = 20

type Draft = { text: string; updated: number }

function parse(value: string | null): Draft | null {
  if (value === null) return null
  try {
    const entry: unknown = JSON.parse(value)
    if (entry && typeof entry === 'object' && 'text' in entry && 'updated' in entry
      && typeof entry.text === 'string' && typeof entry.updated === 'number') return { text: entry.text, updated: entry.updated }
  } catch { /* A corrupt draft reads as empty. */ }
  return null
}

export function readDraft(id: string, storage?: DraftStorage): string {
  try {
    return parse((storage ?? globalThis.localStorage).getItem(PREFIX + id))?.text ?? ''
  } catch { return '' }
}

export function writeDraft(id: string, text: string, storage?: DraftStorage) {
  try {
    const store = storage ?? globalThis.localStorage
    const key = PREFIX + id
    if (!text) { store.removeItem(key); return }
    const added = store.getItem(key) === null
    store.setItem(key, JSON.stringify({ text, updated: Date.now() }))
    if (added) evict(store, key)
  } catch { /* The draft still lives in the open page. */ }
}

function evict(store: DraftStorage, keep: string) {
  const drafts: Array<{ key: string; updated: number }> = []
  for (let index = 0; index < store.length; index++) {
    const key = store.key(index)
    if (key !== null && key !== keep && key.startsWith(PREFIX)) drafts.push({ key, updated: parse(store.getItem(key))?.updated ?? 0 })
  }
  const excess = drafts.length + 1 - DRAFT_LIMIT
  if (excess <= 0) return
  drafts.sort((a, b) => a.updated - b.updated)
  for (const { key } of drafts.slice(0, excess)) store.removeItem(key)
}

export function appendQuote(draft: string, selection: string): string {
  const lines = selection.replace(/\s+$/, '').split(/\r?\n/)
  if (lines.every(line => line.trim() === '')) return draft
  const quote = lines.map(line => line ? `> ${line}` : '>').join('\n') + '\n\n'
  const head = draft.replace(/\s+$/, '')
  return head ? `${head}\n\n${quote}` : quote
}

export function appendQuoteToDraft(id: string, selection: string, storage?: DraftStorage) {
  writeDraft(id, appendQuote(readDraft(id, storage), selection), storage)
}
