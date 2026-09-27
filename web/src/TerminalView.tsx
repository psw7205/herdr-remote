import { useEffect, useRef, useState } from 'react'
import { ArrowDown, ArrowUp, CornerDownLeft, ZoomIn, ZoomOut } from 'lucide-react'
import { sendCommand, terminalFrame, type Session } from './api'
import { Notice } from './Notice'
import { agentName } from './presentation'
import { TopBar } from './TopBar'
import styles from './TerminalView.module.css'
import ui from './ui.module.css'
import { singleFlight } from './singleFlight'
import { withTimeout } from './withTimeout'
import { DEFAULT_CELL_RATIO, fitFontSize, frameDimensions, gridSize, stepZoom, type GridSize } from './terminalSizing'
import '@xterm/xterm/css/xterm.css'

const FONT_FAMILY = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
// A healthy frame takes milliseconds. 6s still covers one Herdr socket call running
// to its 5s timeout plus the 500ms layout read, yet abandons a fetch stalled by a
// network switch long before the browser would, so polling resumes.
const FRAME_TIMEOUT_MS = 6000

export function TerminalView({ session, onBack }: { session: Session; onBack: () => void }) {
  const container = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  const [zoom, setZoom] = useState(1)
  const zoomRef = useRef(zoom)
  const refit = useRef<() => void>(() => {})
  const sender = useRef(Promise.resolve())
  const emit = (type: 'terminal_input' | 'interrupt', text: string) => {
    sender.current = sender.current.then(async () => {
      const response = await sendCommand(session, type, text, crypto.randomUUID())
      if (response.status !== 'accepted') setError(response.status === 'delivery_unknown' ? '입력이 전달됐는지 확인하지 못했습니다. 다시 누르기 전에 화면을 확인하세요.' : '세션이 바뀌었거나 입력이 거부됐습니다.')
    }).catch(() => setError('Terminal 연결이 끊겼습니다. agent는 계속 실행 중입니다.'))
  }
  useEffect(() => { zoomRef.current = zoom; refit.current() }, [zoom])
  useEffect(() => {
    if (!container.current) return
    let active = true
    const unmount = new AbortController()
    let term: import('@xterm/xterm').Terminal | undefined
    let timer: ReturnType<typeof setInterval> | undefined
    let input: { dispose(): void } | undefined
    let observer: ResizeObserver | undefined
    let recent: GridSize[] = []
    let cellRatio = DEFAULT_CELL_RATIO
    // Local xterm font size only; the Herdr PTY keeps the desktop size.
    const fit = () => {
      const shell = container.current
      if (!term || !shell) return
      const style = getComputedStyle(shell)
      const availableWidth = shell.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
      const screen = term.element?.querySelector<HTMLElement>('.xterm-screen')
      const current = term.options.fontSize ?? 0
      // Calibrate the cell width from what xterm actually rendered.
      if (screen && screen.offsetWidth > 0 && current > 0) cellRatio = screen.offsetWidth / (term.cols * current)
      const next = fitFontSize({ availableWidth, cols: term.cols, cellRatio, zoom: zoomRef.current })
      // Skip a sub-step grow when it already fits so calibration cannot ping-pong.
      const fits = !screen || screen.offsetWidth <= availableWidth
      if (fits && next > current && next - current < 0.5) return
      if (Math.abs(next - current) >= 0.25) term.options.fontSize = next
    }
    refit.current = fit
    void import('@xterm/xterm').then(({ Terminal }) => {
      if (!active || !container.current) return
      term = new Terminal({ cols: 120, rows: 40, convertEol: true, scrollback: 0, fontSize: 12, fontFamily: FONT_FAMILY, theme: { background: '#0a0c10', foreground: '#cdd1d6' } })
      term.open(container.current)
      input = term.onData(text => emit('terminal_input', text))
      observer = new ResizeObserver(() => fit())
      observer.observe(container.current)
      const screen = term.element?.querySelector('.xterm-screen')
      if (screen) observer.observe(screen)
      const refresh = singleFlight(async () => {
        try {
          const frame = await withTimeout(signal => terminalFrame(session, signal), FRAME_TIMEOUT_MS, unmount.signal)
          if (!active || !term) return
          const sized = gridSize({ cols: frame.cols, rows: frame.rows }, frameDimensions(frame.text), recent)
          const next = sized.grid
          recent = sized.recent
          if (next.cols !== term.cols || next.rows !== term.rows) { term.resize(next.cols, next.rows); fit() }
          term.reset(); term.write(frame.text); setError('')
        } catch { if (active) setError('Terminal 화면을 읽을 수 없습니다. 목록에서 세션을 다시 여세요.') }
      })
      fit()
      void refresh()
      timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 1000)
    })
    return () => { active = false; unmount.abort(); refit.current = () => {}; if (timer) clearInterval(timer); observer?.disconnect(); input?.dispose(); term?.dispose() }
  }, [session.id, session.runtime_binding])
  // The Terminal stays dark in both themes, like the pane it mirrors.
  return <div className={styles.screen} data-scheme="dark">
    <TopBar
      onBack={onBack}
      backLabel="Chat으로 돌아가기"
      title={session.title || agentName(session.agent)}
      subtitle={<><span className={styles.label}>Terminal</span><span>PC 화면 크기 유지</span></>}
      actions={<div className={styles.zoom} role="group" aria-label="Terminal 글자 크기">
        <button type="button" className={ui.iconButton} onClick={() => setZoom(z => stepZoom(z, -1))} aria-label="글자 작게" disabled={zoom <= 0.5}><ZoomOut aria-hidden size={20} /></button>
        <button type="button" className={styles.fit} onClick={() => setZoom(1)} aria-label="화면 폭에 맞춤" aria-pressed={zoom === 1}>맞춤</button>
        <button type="button" className={ui.iconButton} onClick={() => setZoom(z => stepZoom(z, 1))} aria-label="글자 크게" disabled={zoom >= 3}><ZoomIn aria-hidden size={20} /></button>
      </div>}
    />
    {error && <div className={styles.notice}><Notice tone="warning">{error}</Notice></div>}
    <div className={styles.shell} ref={container} />
    <div className={styles.keys} role="group" aria-label="Terminal 특수 키">
      <button type="button" className={styles.key} onClick={() => emit('terminal_input', '\x1b')}>Esc</button>
      <button type="button" className={styles.key} onClick={() => emit('terminal_input', '\t')}>Tab</button>
      <button type="button" className={styles.key} onClick={() => emit('interrupt', '')}>Ctrl-C</button>
      <button type="button" className={styles.key} onClick={() => emit('terminal_input', '\x1b[A')} aria-label="위 화살표"><ArrowUp aria-hidden size={18} /></button>
      <button type="button" className={styles.key} onClick={() => emit('terminal_input', '\x1b[B')} aria-label="아래 화살표"><ArrowDown aria-hidden size={18} /></button>
      <button type="button" className={styles.key} onClick={() => emit('terminal_input', '\r')} aria-label="Enter"><CornerDownLeft aria-hidden size={18} /></button>
    </div>
  </div>
}
