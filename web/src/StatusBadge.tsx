import { Check } from 'lucide-react'
import type { StatusPresentation, Tone } from './presentation'
import styles from './StatusBadge.module.css'

// The one expressive element: the same marks Herdr draws on the PC, so a
// glance at the phone reads the way a glance at the desktop does.
export function StatusMark({ tone, size = 'small' }: { tone: Tone; size?: 'small' | 'large' }) {
  return <span className={styles.mark} data-tone={tone} data-size={size} aria-hidden="true">
    {tone === 'idle' && <Check size={size === 'large' ? 14 : 12} strokeWidth={3} />}
  </span>
}

export function StatusBadge({ status }: { status: StatusPresentation }) {
  return <span className={styles.badge} data-tone={status.tone}><StatusMark tone={status.tone} /><span>{status.label}</span></span>
}
