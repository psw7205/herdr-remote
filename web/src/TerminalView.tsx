import { useEffect, useRef, useState } from 'react'
import { sendCommand, terminalFrame, type Session } from './api'
import { singleFlight } from './singleFlight'
import { DEFAULT_CELL_RATIO, fitFontSize, frameDimensions, gridSize, keyboardViewport, stepZoom, type GridSize } from './terminalSizing'
import '@xterm/xterm/css/xterm.css'

const FONT_FAMILY = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'

export function TerminalView({ session, onBack }: { session: Session; onBack: () => void }) {
  const layout = useRef<HTMLDivElement>(null)
  const container = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  const [zoom, setZoom] = useState(1)
  const zoomRef = useRef(zoom)
  const refit = useRef<() => void>(() => {})
  const sender = useRef(Promise.resolve())
  const emit = (type: 'terminal_input' | 'interrupt', text: string) => {
    sender.current = sender.current.then(async () => {
      const response = await sendCommand(session, type, text, crypto.randomUUID())
      if (response.status !== 'accepted') setError(response.status === 'delivery_unknown' ? '입력 전달 여부가 불확실합니다. 다시 보내기 전에 PC 화면을 확인하세요.' : '세션이 바뀌었거나 입력이 거부됐습니다.')
    }).catch(() => setError('Terminal 연결이 끊겼습니다. agent는 계속 실행 중입니다.'))
  }
  useEffect(() => { zoomRef.current = zoom; refit.current() }, [zoom])
  // Keep the key bar above the on-screen keyboard: 100dvh does not shrink for
  // it on iOS, but the visual viewport does. Pinch zoom is not tracked.
  useEffect(() => {
    const viewport = window.visualViewport
    const element = layout.current
    if (!viewport || !element) return
    const update = () => {
      const visible = keyboardViewport(viewport)
      if (!visible) return
      element.style.setProperty('--terminal-height', `${visible.height}px`)
      element.style.setProperty('--terminal-top', `${visible.top}px`)
    }
    update()
    viewport.addEventListener('resize', update)
    viewport.addEventListener('scroll', update)
    return () => { viewport.removeEventListener('resize', update); viewport.removeEventListener('scroll', update) }
  }, [])
  useEffect(() => {
    if (!container.current) return
    let active = true
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
      term = new Terminal({ cols: 120, rows: 40, convertEol: true, scrollback: 0, fontSize: 12, fontFamily: FONT_FAMILY, theme: { background: '#0b1120', foreground: '#e7eef7' } })
      term.open(container.current)
      input = term.onData(text => emit('terminal_input', text))
      observer = new ResizeObserver(() => fit())
      observer.observe(container.current)
      const screen = term.element?.querySelector('.xterm-screen')
      if (screen) observer.observe(screen)
      const refresh = singleFlight(async () => {
        try {
          const frame = await terminalFrame(session)
          if (!active || !term) return
          const sized = gridSize({ cols: frame.cols, rows: frame.rows }, frameDimensions(frame.text), recent)
          const next = sized.grid
          recent = sized.recent
          if (next.cols !== term.cols || next.rows !== term.rows) { term.resize(next.cols, next.rows); fit() }
          term.reset(); term.write(frame.text); setError('')
        } catch { if (active) setError('Terminal 화면을 읽을 수 없습니다. 세션을 다시 선택하세요.') }
      })
      fit()
      void refresh()
      timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 1000)
    })
    return () => { active = false; refit.current = () => {}; if (timer) clearInterval(timer); observer?.disconnect(); input?.dispose(); term?.dispose() }
  }, [session.id, session.runtime_binding])
  return <div className="session-layout terminal-layout" ref={layout}>
    <header className="topbar"><button className="plain back" onClick={onBack} aria-label="Chat으로 돌아가기">‹</button><div className="session-heading"><strong>Terminal</strong><small>기존 Herdr pane · 화면 크기 유지</small></div>
      <div className="terminal-zoom" aria-label="Terminal 글자 크기">
        <button onClick={() => setZoom(z => stepZoom(z, -1))} aria-label="글자 작게" disabled={zoom <= 0.5}>A−</button>
        <button onClick={() => setZoom(1)} aria-label="화면 폭에 맞춤" aria-pressed={zoom === 1}>맞춤</button>
        <button onClick={() => setZoom(z => stepZoom(z, 1))} aria-label="글자 크게" disabled={zoom >= 3}>A+</button>
      </div>
    </header>
    {error && <div className="notice" role="alert">{error}</div>}
    <div className="terminal-shell" ref={container} />
    <div className="terminal-keys" aria-label="Terminal 특수 키">
      <button onClick={() => emit('terminal_input', '\x1b')}>Esc</button><button onClick={() => emit('terminal_input', '\t')}>Tab</button><button onClick={() => emit('interrupt', '')}>Ctrl-C</button><button onClick={() => emit('terminal_input', '\x1b[A')}>↑</button><button onClick={() => emit('terminal_input', '\x1b[B')}>↓</button><button onClick={() => emit('terminal_input', '\r')}>Enter</button>
    </div>
  </div>
}
