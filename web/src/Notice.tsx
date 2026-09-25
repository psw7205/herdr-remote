import type { ReactNode } from 'react'
import { CircleAlert, Info, TriangleAlert } from 'lucide-react'
import styles from './Notice.module.css'
import ui from './ui.module.css'

export type NoticeTone = 'info' | 'warning' | 'danger'
export type NoticeAction = { label: string; onClick: () => void }

const icons = { info: Info, warning: TriangleAlert, danger: CircleAlert }

export function Notice({ tone, children, actions = [] }: { tone: NoticeTone; children: ReactNode; actions?: NoticeAction[] }) {
  const Icon = icons[tone]
  return <div className={styles.notice} data-tone={tone} role={tone === 'info' ? 'status' : 'alert'}>
    <Icon className={styles.icon} aria-hidden size={18} />
    <div className={styles.body}>
      <p>{children}</p>
      {actions.length > 0 && <div className={styles.actions}>{actions.map(action => <button key={action.label} type="button" className={ui.button} onClick={action.onClick}>{action.label}</button>)}</div>}
    </div>
  </div>
}
