import { memo, useCallback, useEffect, useRef, useState } from 'react'
import { FileDiff as FileDiffIcon, FolderX, GitBranch, RefreshCw } from 'lucide-react'
import { getChanges, getFileDiff, type ChangedFile, type Changes, type FileDiff, type Session } from './api'
import { changesError, hasLineChanges, parseDiff, splitPath, statusLabel, totals, type DiffLine } from './changes'
import { Notice } from './Notice'
import { projectName } from './presentation'
import { formatRoute } from './route'
import { TopBar } from './TopBar'
import styles from './ChangesView.module.css'
import ui from './ui.module.css'

type Load<T> = { data: T | null; error: string; loading: boolean }

// The last listing per session, so returning from a diff shows it at once
// while the fresh one loads. It never outlives the page.
const lastListing = new Map<string, Changes>()

// useFetch loads on mount and on reload(); an older answer never replaces a
// newer one. The Bridge keeps no watcher, so this is the only refresh.
function useFetch<T>(load: (signal: AbortSignal) => Promise<T>, initial: T | null): Load<T> & { reload: () => void } {
  const [state, setState] = useState<Load<T>>({ data: initial, error: '', loading: true })
  const [generation, setGeneration] = useState(0)
  const loader = useRef(load)
  loader.current = load
  useEffect(() => {
    const abort = new AbortController()
    setState(current => ({ ...current, loading: true }))
    loader.current(abort.signal).then(
      data => { if (!abort.signal.aborted) setState({ data, error: '', loading: false }) },
      error => { if (!abort.signal.aborted) setState(current => ({ data: current.data, error: changesError(error), loading: false })) },
    )
    return () => abort.abort()
  }, [generation])
  const reload = useCallback(() => setGeneration(value => value + 1), [])
  return { ...state, reload }
}

function RefreshButton({ loading, onClick }: { loading: boolean; onClick: () => void }) {
  return <button type="button" className={ui.iconButton} onClick={onClick} disabled={loading} aria-label="새로 고침" title="새로 고침">
    <RefreshCw aria-hidden size={20} className={loading ? styles.spinning : undefined} />
  </button>
}

export function StatusTag({ status }: { status: ChangedFile['status'] }) {
  return <span className={styles.tag} data-status={status}>{statusLabel(status)}</span>
}

function Counts({ file }: { file: ChangedFile }) {
  if (file.binary) return <span className={styles.counts}><span className={styles.binary}>binary</span></span>
  if (file.additions === null || file.deletions === null) return null
  return <span className={styles.counts}>
    <span className={styles.added}>+{file.additions}<span className="visually-hidden">줄 추가</span></span>
    <span className={styles.deleted}>−{file.deletions}<span className="visually-hidden">줄 삭제</span></span>
  </span>
}

// ChangedFiles shows Git's view of the project, which can include edits made
// by a person in the same work tree, so nothing here names the agent.
export function ChangedFiles({ session, onBack, onOpen }: { session: Session; onBack: () => void; onOpen: (path: string) => void }) {
  const { data, error, loading, reload } = useFetch(signal => getChanges(session.id, signal).then(changes => { lastListing.set(session.id, changes); return changes }), lastListing.get(session.id) ?? null)
  const subtitle = <>
    <span>{projectName(session.project)}</span>
    {data?.repository && data.branch && <span className={styles.branch}><GitBranch aria-hidden size={14} />{data.branch}</span>}
  </>
  return <div className={styles.screen}>
    <TopBar onBack={onBack} backLabel="Chat으로 돌아가기" title="변경된 파일" subtitle={subtitle} actions={<RefreshButton loading={loading} onClick={reload} />} />
    {(error || data?.truncated) && <div className={styles.notices}>
      {error && <Notice tone="danger" actions={[{ label: '다시 시도', onClick: reload }]}>{error}</Notice>}
      {data?.truncated && <Notice tone="info">변경된 파일이 많아 처음 {data.files.length}개만 표시합니다. 전체는 {data.total}개 이상입니다.</Notice>}
    </div>}
    <main className={styles.scroller} aria-busy={loading} aria-label="변경된 파일 목록">
      {!data && loading && <div className={styles.skeleton} aria-hidden="true">{[0, 1, 2, 3].map(index => <span key={index}><i /><b /></span>)}</div>}
      {data && <ChangesBody session={session} changes={data} onOpen={onOpen} />}
    </main>
  </div>
}

const DiffLines = memo(function DiffLines({ lines }: { lines: DiffLine[] }) {
  return <div className={styles.code}>
    {lines.map((line, index) => <div key={index} className={styles.line} data-kind={line.kind}>
      <span className={styles.number} aria-hidden="true">{'number' in line ? line.number : ''}</span>
      <span className={styles.mark} aria-hidden="true">{line.kind === 'add' ? '+' : line.kind === 'del' ? '−' : ''}</span>
      <span className={styles.text}>{line.kind === 'add' && <span className="visually-hidden">추가: </span>}{line.kind === 'del' && <span className="visually-hidden">삭제: </span>}{line.text || ' '}</span>
    </div>)}
  </div>
})

export function ChangesBody({ session, changes, onOpen }: { session: Pick<Session, 'id' | 'project'>; changes: Changes; onOpen: (path: string) => void }) {
  const sum = totals(changes.files)
  return <>
    {!changes.repository && <div className={styles.empty}>
      <FolderX aria-hidden size={28} />
      <strong>Git repository가 아닙니다.</strong>
      <p>이 세션의 폴더({projectName(session.project)})는 Git으로 관리되지 않아 변경된 파일을 보여줄 수 없습니다.</p>
    </div>}
    {changes.repository && changes.files.length === 0 && <div className={styles.empty}>
      <FileDiffIcon aria-hidden size={28} />
      <strong>변경된 파일이 없습니다.</strong>
      <p>{changes.initial ? '아직 commit이 없고 추가된 파일도 없습니다.' : 'working tree가 마지막 commit과 같습니다.'}</p>
    </div>}
    {changes.repository && changes.files.length > 0 && <>
      <p className={styles.summary}>
        <span>파일 {changes.files.length}개</span>
        <span className={styles.added}>+{sum.additions}</span>
        <span className={styles.deleted}>−{sum.deletions}</span>
        <span className={styles.scope}>{changes.initial ? '아직 commit 없음' : '마지막 commit 기준'} · 사람이 고친 내용도 함께 보입니다</span>
      </p>
      <ul className={styles.rows}>
        {changes.files.map(file => {
          const { dir, name } = splitPath(file.path)
          return <li key={file.path}>
            <a className={styles.row} href={formatRoute({ session: session.id, view: 'changes', file: file.path })} onClick={event => {
              if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
              event.preventDefault()
              onOpen(file.path)
            }}>
              <StatusTag status={file.status} />
              <span className={styles.path}>
                <span className={styles.name}>{name}</span>
                {dir && <span className={styles.dir}>{dir}</span>}
                {file.old_path && <span className={styles.dir}>← {file.old_path}</span>}
              </span>
              <Counts file={file} />
            </a>
          </li>
        })}
      </ul>
    </>}
  </>
}

export function DiffBody({ diff }: { diff: FileDiff }) {
  const lines = diff.content === 'text' ? parseDiff(diff.diff) : []
  return <>
    {(diff.binary || diff.additions !== null) && <div className={styles.summary}><Counts file={diff} /></div>}
    {diff.content === 'binary' && <div className={styles.empty}><strong>binary 파일입니다.</strong><p>내용은 표시하지 않습니다.</p></div>}
    {diff.content === 'none' && <div className={styles.empty}><strong>내용을 읽지 않는 항목입니다.</strong><p>symlink나 다른 repository처럼 일반 파일이 아닌 항목은 내용을 표시하지 않습니다.</p></div>}
    {diff.content === 'text' && !hasLineChanges(lines) && <div className={styles.empty}><strong>표시할 줄 변경이 없습니다.</strong><p>빈 파일이거나 권한(mode)만 바뀌었습니다.</p></div>}
    {diff.content === 'text' && lines.length > 0 && <DiffLines lines={lines} />}
  </>
}

export function FileDiffView({ session, path, onBack }: { session: Session; path: string; onBack: () => void }) {
  const { data, error, loading, reload } = useFetch<FileDiff>(signal => getFileDiff(session.id, path, signal), null)
  const { dir, name } = splitPath(path)
  const subtitle = <>
    {data && <StatusTag status={data.status} />}
    <span>{data?.old_path ? `${data.old_path} → ${path}` : dir || projectName(session.project)}</span>
  </>
  return <div className={styles.screen}>
    <TopBar onBack={onBack} backLabel="변경된 파일 목록" title={name} subtitle={subtitle} actions={<RefreshButton loading={loading} onClick={reload} />} />
    {(error || data?.truncated) && <div className={styles.notices}>
      {error && <Notice tone="danger" actions={[{ label: '다시 시도', onClick: reload }, { label: '목록으로', onClick: onBack }]}>{error}</Notice>}
      {data?.truncated && <Notice tone="info">diff가 커서 앞부분만 표시합니다. 나머지는 PC에서 확인하세요.</Notice>}
    </div>}
    <main className={styles.scroller} aria-busy={loading} aria-label={`${name} diff`}>
      {!data && loading && <div className={styles.skeleton} aria-hidden="true">{[0, 1, 2, 3].map(index => <span key={index}><b /></span>)}</div>}
      {data && <DiffBody diff={data} />}
    </main>
  </div>
}
