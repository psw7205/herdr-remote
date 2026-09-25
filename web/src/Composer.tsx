import { useLayoutEffect, useRef } from 'react'
import { ArrowUp, Check, CircleAlert, Info, LoaderCircle, TriangleAlert } from 'lucide-react'
import type { NoticeAction } from './Notice'
import styles from './Composer.module.css'
import ui from './ui.module.css'

// progress is this client's own request in flight; working is the agent's.
export type ComposerStatus = { tone: 'neutral' | 'progress' | 'working' | 'success' | 'warning' | 'danger'; text: string; actions?: NoticeAction[] }
const icons = { neutral: Info, progress: LoaderCircle, working: LoaderCircle, success: Check, warning: TriangleAlert, danger: CircleAlert }
// About six lines; the field scrolls beyond that.
const MAX_HEIGHT = 168

// The send button never turns into Stop: a status poll can change the agent's
// state under a finger, and a mistaken interrupt ends a turn. Stop is offered
// as a status action instead.
export function Composer({ draft, onDraft, canType, canSend: sendable, sending, onSend, status }: {
  draft: string
  onDraft: (text: string) => void
  canType: boolean
  canSend: boolean
  sending: boolean
  onSend: () => void
  status: ComposerStatus | null
}) {
  const field = useRef<HTMLTextAreaElement>(null)
  useLayoutEffect(() => {
    const element = field.current
    if (!element) return
    element.style.height = 'auto'
    element.style.height = `${Math.min(element.scrollHeight, MAX_HEIGHT)}px`
  }, [draft])
  const Icon = status ? icons[status.tone] : null
  const canSend = sendable && !sending && draft.trim().length > 0
  return <form className={styles.composer} onSubmit={event => { event.preventDefault(); onSend() }}>
    {status && Icon && <div className={styles.status} data-tone={status.tone} role="status" aria-live="polite">
      <Icon className={styles.statusIcon} aria-hidden size={16} />
      <div className={styles.statusBody}>
        <p>{status.text}</p>
        {status.actions && status.actions.length > 0 && <div className={styles.statusActions}>{status.actions.map(action => <button key={action.label} type="button" className={ui.button} onClick={action.onClick}>{action.label}</button>)}</div>}
      </div>
    </div>}
    <div className={styles.row}>
      <textarea
        ref={field}
        className={styles.input}
        aria-label="메시지"
        placeholder="이어서 요청하기…"
        value={draft}
        rows={1}
        disabled={!canType || sending}
        onChange={event => onDraft(event.target.value)}
        onKeyDown={event => { if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); onSend() } }}
      />
      <button type="submit" className={styles.primary} aria-label={sending ? '전달하는 중' : '보내기'} disabled={!canSend}>{sending ? <LoaderCircle className={styles.spin} aria-hidden size={20} /> : <ArrowUp aria-hidden size={20} strokeWidth={2.5} />}</button>
    </div>
  </form>
}
