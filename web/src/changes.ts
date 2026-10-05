import type { ChangedFile, ChangeStatus } from './api'

const statusLabels: Record<ChangeStatus, string> = { modified: '수정', added: '추가', deleted: '삭제', renamed: '이름 변경' }
export function statusLabel(status: ChangeStatus): string {
  return statusLabels[status] ?? '변경'
}

export function splitPath(path: string): { dir: string; name: string } {
  const index = path.lastIndexOf('/')
  return index < 0 ? { dir: '', name: path } : { dir: path.slice(0, index + 1), name: path.slice(index + 1) }
}

// Sums the counts the Bridge knows; a binary file or a cut listing adds nothing.
export function totals(files: ChangedFile[]): { additions: number; deletions: number } {
  return files.reduce((sum, file) => ({ additions: sum.additions + (file.additions ?? 0), deletions: sum.deletions + (file.deletions ?? 0) }), { additions: 0, deletions: 0 })
}

const errorMessages: Record<string, string> = {
  SESSION_NOT_FOUND: '세션을 찾을 수 없습니다. 목록에서 다시 여세요.',
  PROJECT_NOT_FOUND: '프로젝트 폴더를 찾을 수 없습니다. 폴더가 옮겨졌거나 삭제됐을 수 있습니다.',
  NOT_A_REPOSITORY: '이 프로젝트는 Git repository가 아닙니다.',
  FILE_NOT_CHANGED: '이 파일은 더 이상 변경된 파일 목록에 없습니다.',
  GIT_UNAVAILABLE: 'Bridge가 실행되는 PC에서 git을 찾을 수 없습니다.',
  GIT_TIMEOUT: 'Git 조회가 오래 걸려 중단했습니다. 잠시 후 다시 시도하세요.',
}
export function changesError(error: unknown): string {
  const code = error instanceof Error ? error.message : ''
  return errorMessages[code] ?? '변경된 파일을 읽지 못했습니다. 잠시 후 다시 시도하세요.'
}

export type DiffLine =
  | { kind: 'add' | 'del' | 'context'; text: string; number: number }
  | { kind: 'hunk' | 'meta' | 'note'; text: string }

const hunkHeader = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/
// The file header repeats what the screen already shows; other lines before
// a hunk (rename, mode, "Binary files … differ") stay as meta.
const fileHeader = /^(diff --git |index |--- |\+\+\+ )/

// parseDiff reads a unified diff by hunk line counts, so a removed line that
// starts with "-- " or an added "++ " is never taken for a file header. Each
// line carries the number a reader looks up: the new line for an addition or
// context, the old line for a deletion.
export function parseDiff(text: string): DiffLine[] {
  const lines = text.split('\n')
  if (lines.at(-1) === '') lines.pop()
  const out: DiffLine[] = []
  let oldLine = 0
  let newLine = 0
  let oldLeft = 0
  let newLeft = 0
  for (const line of lines) {
    if (oldLeft > 0 || newLeft > 0) {
      const mark = line[0]
      if (mark === '+') { out.push({ kind: 'add', text: line.slice(1), number: newLine++ }); newLeft -= 1 }
      else if (mark === '-') { out.push({ kind: 'del', text: line.slice(1), number: oldLine++ }); oldLeft -= 1 }
      else if (mark === '\\') out.push({ kind: 'note', text: line })
      else { out.push({ kind: 'context', text: line.slice(1), number: newLine }); oldLine += 1; newLine += 1; oldLeft -= 1; newLeft -= 1 }
      continue
    }
    const hunk = hunkHeader.exec(line)
    if (hunk) {
      oldLine = Number(hunk[1]); oldLeft = hunk[2] === undefined ? 1 : Number(hunk[2])
      newLine = Number(hunk[3]); newLeft = hunk[4] === undefined ? 1 : Number(hunk[4])
      out.push({ kind: 'hunk', text: line })
    } else if (line.startsWith('\\')) out.push({ kind: 'note', text: line })
    else if (!fileHeader.test(line)) out.push({ kind: 'meta', text: line })
  }
  return out
}

export function hasLineChanges(lines: DiffLine[]): boolean {
  return lines.some(line => line.kind === 'add' || line.kind === 'del')
}
