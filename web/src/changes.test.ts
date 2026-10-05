import { describe, expect, it } from 'vitest'
import type { ChangedFile } from './api'
import { changesError, hasLineChanges, parseDiff, splitPath, statusLabel, totals } from './changes'

describe('changed file presentation', () => {
  it('labels each status without naming who changed it', () => {
    expect(['modified', 'added', 'deleted', 'renamed'].map(status => statusLabel(status as ChangedFile['status']))).toEqual(['수정', '추가', '삭제', '이름 변경'])
  })
  it('splits a path into its folder and name', () => {
    expect(splitPath('src/charts/Line.tsx')).toEqual({ dir: 'src/charts/', name: 'Line.tsx' })
    expect(splitPath('README.md')).toEqual({ dir: '', name: 'README.md' })
  })
  it('sums only the counts the Bridge knows', () => {
    const files: ChangedFile[] = [
      { path: 'a', status: 'modified', additions: 3, deletions: 1 },
      { path: 'b', status: 'modified', binary: true, additions: null, deletions: null },
      { path: 'c', status: 'added', untracked: true, additions: 10, deletions: 0 },
    ]
    expect(totals(files)).toEqual({ additions: 13, deletions: 1 })
  })
  it('explains a Bridge error code and falls back for anything else', () => {
    expect(changesError(new Error('PROJECT_NOT_FOUND'))).toContain('프로젝트 폴더')
    expect(changesError(new Error('GIT_FAILED'))).toContain('읽지 못했습니다')
    expect(changesError('x')).toContain('읽지 못했습니다')
  })
})

describe('unified diff', () => {
  it('numbers lines by hunk and hides the file header', () => {
    const lines = parseDiff('diff --git a/x b/x\nindex 1..2 100644\n--- a/x\n+++ b/x\n@@ -3,3 +3,4 @@ fn\n keep\n-old\n+new\n+added\n tail\n')
    expect(lines).toEqual([
      { kind: 'hunk', text: '@@ -3,3 +3,4 @@ fn' },
      { kind: 'context', text: 'keep', number: 3 },
      { kind: 'del', text: 'old', number: 4 },
      { kind: 'add', text: 'new', number: 4 },
      { kind: 'add', text: 'added', number: 5 },
      { kind: 'context', text: 'tail', number: 6 },
    ])
  })
  it('keeps hunk lines that look like file headers inside a hunk', () => {
    const lines = parseDiff('@@ -1,2 +1,2 @@\n--- not a header\n+++ not a header\n-x\n+y\n')
    expect(lines.map(line => line.kind)).toEqual(['hunk', 'del', 'add', 'del', 'add'])
    expect(lines[1]).toMatchObject({ text: '-- not a header' })
  })
  it('reads one-line hunks, missing final newlines and rename meta', () => {
    const lines = parseDiff('diff --git a/a b/b\nsimilarity index 90%\nrename from a\nrename to b\n@@ -0,0 +1 @@\n+only\n\\ No newline at end of file\n')
    expect(lines.map(line => line.kind)).toEqual(['meta', 'meta', 'meta', 'hunk', 'add', 'note'])
    expect(lines[4]).toEqual({ kind: 'add', text: 'only', number: 1 })
  })
  it('stops cleanly at a diff cut mid-hunk', () => {
    const lines = parseDiff('@@ -1,100 +1,100 @@\n a\n-b\n')
    expect(lines.map(line => line.kind)).toEqual(['hunk', 'context', 'del'])
  })
  it('tells a diff with line changes from a mode-only or empty one', () => {
    expect(hasLineChanges(parseDiff('diff --git a/x b/x\nold mode 100644\nnew mode 100755\n'))).toBe(false)
    expect(hasLineChanges(parseDiff(''))).toBe(false)
    expect(hasLineChanges(parseDiff('@@ -1 +1 @@\n-a\n+b\n'))).toBe(true)
  })
})
