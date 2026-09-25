import type { ReactNode } from 'react'
import { ArrowLeft } from 'lucide-react'
import styles from './TopBar.module.css'
import ui from './ui.module.css'

export function TopBar({ title, subtitle, onBack, backLabel, actions }: { title: ReactNode; subtitle?: ReactNode; onBack?: () => void; backLabel?: string; actions?: ReactNode }) {
  return <header className={styles.bar} data-back={onBack ? '' : undefined}>
    {onBack && <button type="button" className={ui.iconButton} onClick={onBack} aria-label={backLabel}><ArrowLeft aria-hidden size={22} /></button>}
    <div className={styles.heading}>
      <h1 className={styles.title}>{title}</h1>
      {subtitle && <div className={styles.subtitle}>{subtitle}</div>}
    </div>
    {actions && <div className={styles.actions}>{actions}</div>}
  </header>
}
