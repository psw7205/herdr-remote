import { useCallback, useEffect, useRef, useState } from 'react'
import { FolderGit2, LoaderCircle, RefreshCw } from 'lucide-react'
import { listStartCandidates, startSession, type StartCandidate, type StartCapability } from './api'
import { Notice, type NoticeAction } from './Notice'
import { chooseStart, groupCandidates, noRootNotice, permissionNotice, placementLabel, readPendingStart, startOutcome, uncertainStart, writePendingStart, type PendingStart, type StartOutcome } from './sessionStart'
import { TopBar } from './TopBar'
import { withTimeout } from './withTimeout'
import styles from './StartSession.module.css'
import ui from './ui.module.css'

// Herdr answers after its agent readiness wait (up to 30s); the Bridge bounds
// a start at 50s. Waiting longer keeps a normal start from looking uncertain.
const START_TIMEOUT_MS = 60_000
const KIND = 'claude'

export function StartSession({ start, onBack }: { start: StartCapability; onBack: () => void }) {
  const [candidates, setCandidates] = useState<StartCandidate[]>([])
  // The candidate listing carries the capability too, so a reload straight
  // into this screen does not wait for the session list.
  const [capability, setCapability] = useState(start)
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [pending, setPending] = useState<PendingStart | null>(() => readPendingStart())
  const [outcome, setOutcome] = useState<StartOutcome | null>(() => readPendingStart() ? uncertainStart : null)
  const [busy, setBusy] = useState(false)
  const unmount = useRef(new AbortController())
  useEffect(() => () => unmount.current.abort(), [])

  const load = useCallback(async () => {
    try {
      const body = await listStartCandidates()
      setCandidates(body.candidates); setCapability(body.start); setLoadError('')
    } catch { setLoadError('폴더 목록을 불러오지 못했습니다. Herdr 연결을 확인한 뒤 다시 시도하세요.') }
    setLoaded(true)
  }, [])
  useEffect(() => { void load() }, [load])

  const send = useCallback(async (intent: PendingStart) => {
    writePendingStart(intent); setPending(intent); setBusy(true); setOutcome(null)
    try {
      const result = await withTimeout(signal => startSession(intent.candidate, intent.kind, intent.placement, intent.id, signal), START_TIMEOUT_MS, unmount.current.signal)
      const next = startOutcome(result)
      if (next.settled) { writePendingStart(null); setPending(null); setSelected(null) }
      setOutcome(next)
      void load()
    } catch {
      // A lost answer is not a non-delivery: keep the ID for a re-check.
      if (!unmount.current.signal.aborted) setOutcome(uncertainStart)
    } finally {
      if (!unmount.current.signal.aborted) setBusy(false)
    }
  }, [load])

  const chosen = candidates.find(item => item.id === selected)
  const submit = () => {
    if (!chosen) return
    const intent = chooseStart(pending, chosen, KIND, () => crypto.randomUUID())
    if (!intent) { setOutcome({ ...uncertainStart, text: '이전 시작 요청의 결과를 아직 모릅니다. 결과를 확인한 뒤 새로 시작하세요.' }); return }
    void send(intent)
  }
  const actions: NoticeAction[] = []
  if (pending && !busy) {
    actions.push({ label: '결과 다시 확인', onClick: () => void send(pending) })
    actions.push({ label: 'PC에서 확인함', onClick: () => { writePendingStart(null); setPending(null); setOutcome(null) } })
  }
  const allowed = capability.kinds.includes(KIND)
  const groups = groupCandidates(candidates)
  const pendingOther = pending !== null && pending.candidate !== selected

  return <div className={styles.screen}>
    <TopBar title="새 세션" subtitle={<span>Claude · Herdr에서 시작</span>} onBack={onBack} backLabel="세션 목록으로"
      actions={<button type="button" className={ui.iconButton} onClick={() => void load()} aria-label="폴더 목록 새로 고침" disabled={busy}><RefreshCw aria-hidden size={20} /></button>} />
    <div className={styles.notices}>
      {outcome && <Notice tone={outcome.tone} actions={actions}>{outcome.text}</Notice>}
      {loaded && !allowed && <Notice tone="warning">이 Bridge에서는 새 세션을 시작할 수 없습니다.</Notice>}
      {loadError && <Notice tone="danger">{loadError}</Notice>}
      <Notice tone="info">{permissionNotice}</Notice>
      {loaded && !capability.new_workspace && <Notice tone="info">{noRootNotice}</Notice>}
    </div>
    <main className={styles.list} aria-busy={!loaded} aria-label="시작할 폴더">
      {loaded && groups.length === 0 && !loadError && <div className={styles.empty}>
        <FolderGit2 aria-hidden size={28} />
        <strong>시작할 수 있는 폴더가 없습니다.</strong>
        <p>-project-root 아래의 Git repo나 PC에서 연 workspace가 여기에 나타납니다.</p>
      </div>}
      {groups.map(group => <section key={group.key} aria-labelledby={`start-${group.key}`}>
        <h2 id={`start-${group.key}`} className={styles.groupTitle}>{group.title}</h2>
        <ul className={styles.rows}>
          {group.candidates.map(item => <li key={item.id}>
            <button type="button" className={styles.row} aria-pressed={selected === item.id} disabled={busy || !allowed} onClick={() => setSelected(item.id)}>
              <FolderGit2 aria-hidden size={18} className={styles.icon} />
              <span className={styles.body}>
                <strong className={styles.name}>{item.name}</strong>
                <span className={styles.meta}>{placementLabel(item)}</span>
              </span>
            </button>
          </li>)}
        </ul>
      </section>)}
    </main>
    <footer className={styles.footer}>
      <button type="button" className={styles.primary} disabled={!chosen || busy || !allowed || pendingOther} onClick={submit}>
        {busy ? <><LoaderCircle aria-hidden size={18} className={styles.spin} />시작하는 중… 최대 30초</> : chosen ? `${chosen.name}에서 Claude 시작` : '폴더를 고르세요'}
      </button>
    </footer>
  </div>
}
