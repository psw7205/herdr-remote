import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { Changes, FileDiff, Session } from './api'
import { ChangesBody, DiffBody } from './ChangesView'
import { SessionChat } from './SessionChat'

const session = { id: 'claude:a', project: '/work/sample' }
const changes: Changes = {
  repository: true, branch: 'main', total: 4, truncated: false, files: [
    { path: 'src/a.ts', status: 'modified', additions: 3, deletions: 1 },
    { path: 'src/new.ts', status: 'added', untracked: true, additions: 5, deletions: 0 },
    { path: 'old.txt', status: 'deleted', additions: 0, deletions: 2 },
    { path: 'img/logo.png', status: 'modified', binary: true, additions: null, deletions: null },
  ],
}
const list = (value: Changes) => renderToStaticMarkup(<ChangesBody session={session} changes={value} onOpen={() => {}} />)
const diff = (value: Partial<FileDiff>) => renderToStaticMarkup(<DiffBody diff={{ path: 'src/a.ts', status: 'modified', additions: 1, deletions: 1, content: 'text', diff: '', truncated: false, ...value }} />)

describe('changed file list', () => {
  it('shows each status, the counts and links to the file diff', () => {
    const html = list(changes)
    for (const label of ['>수정<', '>추가<', '>삭제<']) expect(html).toContain(label)
    expect(html).toContain('파일 4개')
    expect(html).toContain('+8')
    expect(html).toContain('−3')
    expect(html).toContain('>binary<')
    expect(html).toContain('href="#session=claude%3Aa&amp;view=changes&amp;file=src%2Fnew.ts"')
  })
  it('never attributes the changes to an agent', () => {
    const html = list(changes)
    expect(html).toContain('사람이 고친 내용도 함께 보입니다')
    expect(html).not.toMatch(/Claude|Codex|agent/)
  })
  it('names the empty, initial and non-repository states', () => {
    expect(list({ ...changes, files: [], total: 0 })).toContain('변경된 파일이 없습니다.')
    expect(list({ ...changes, files: [], total: 0, initial: true })).toContain('아직 commit이 없고')
    expect(list({ repository: false, files: [], total: 0, truncated: false })).toContain('Git repository가 아닙니다.')
  })
})

describe('file diff body', () => {
  it('styles additions and deletions by line kind', () => {
    const html = diff({ diff: '@@ -1 +1 @@\n-old\n+new\n' })
    expect(html).toMatch(/data-kind="del"[^]*삭제: <\/span>old/)
    expect(html).toMatch(/data-kind="add"[^]*추가: <\/span>new/)
  })
  it('says why a diff has no lines', () => {
    expect(diff({ content: 'binary', binary: true, additions: null, deletions: null })).toContain('binary 파일입니다.')
    expect(diff({ content: 'none', additions: null, deletions: null })).toContain('내용을 읽지 않는 항목입니다.')
    expect(diff({ diff: 'diff --git a/x b/x\nold mode 100644\nnew mode 100755\n' })).toContain('표시할 줄 변경이 없습니다.')
  })
})

describe('entry from Chat', () => {
  beforeEach(() => { vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: () => {}, removeItem: () => {} }) })
  afterEach(() => vi.unstubAllGlobals())
  const chat: Session = { id: 'codex:a', agent: 'codex', pane_id: 'w1:p1', project: '/work/a', title: 'A', status: 'idle', chat: true, terminal: false, active: true, lifecycle: 'unbound' }
  it('offers Changed Files even for a read-only session without a binding', () => {
    expect(renderToStaticMarkup(<SessionChat initial={chat} onBack={() => {}} onTerminal={() => {}} onChanges={() => {}} />)).toContain('aria-label="변경된 파일"')
  })
})
