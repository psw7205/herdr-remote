import { useEffect, useRef, useState, type MouseEvent, type PointerEvent } from 'react'
import { ArrowDown, ArrowUp, CornerDownLeft, ZoomIn, ZoomOut } from 'lucide-react'
import { sendCommand, terminalFrame, type Session } from './api'
import { startHold, type Hold } from './holdRepeat'
import { Notice } from './Notice'
import { agentName } from './presentation'
import { TopBar } from './TopBar'
import styles from './TerminalView.module.css'
import ui from './ui.module.css'
import { singleFlight } from './singleFlight'
import { attachImeInput } from './terminalInput'
import { withTimeout } from './withTimeout'
import { DEFAULT_CELL_RATIO, fitFontSize, fontSizeFor, frameDimensions, gridSize, MAX_FONT_SIZE, parseFontChoice, pinchFont, revealScroll, stepFont, type FontChoice, type GridSize } from './terminalSizing'
import '@xterm/xterm/css/xterm.css'

const FONT_FAMILY = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
// A healthy frame takes milliseconds. 6s still covers one Herdr socket call running
// to its 5s timeout plus the 500ms layout read, yet abandons a fetch stalled by a
// network switch long before the browser would, so polling resumes.
const FRAME_TIMEOUT_MS = 6000
const HOLD_DELAY_MS = 400
const HOLD_INTERVAL_MS = 120
const DRAG_OFF_PX = 12
const FONT_KEY = 'terminal-font'

function storedFont(): FontChoice {
  try { return parseFontChoice(localStorage.getItem(FONT_KEY)) } catch { return 'fit' }
}
function storeFont(choice: FontChoice) {
  try { if (choice === 'fit') localStorage.removeItem(FONT_KEY); else localStorage.setItem(FONT_KEY, String(choice)) } catch { /* the choice still applies to this view */ }
}

type Cell = { width: number; height: number }
type PinchStart = { distance: number; font: number; cell: Cell; midX: number; midY: number; scrollLeft: number; scrollTop: number }

// The reset travels in the same write as the frame (RIS, ESC c): Terminal.reset()
// clears synchronously while write() is parsed on a later task, so a render in
// between showed an empty Terminal on every refresh.
// Herdr frames carry no cursor position, so xterm's cursor would sit where the
// frame text ends, for an agent the end of its status row, nowhere near its
// input. So the cursor is hidden and parked at the start of the last line,
// where input begins: xterm keeps its input textarea on the cursor, and the
// browser scrolls that textarea into view as the IME types.
function redraw(frame: string): string {
  return `\x1bc${frame}\r\x1b[?25l`
}

export function repaint(painted: string | null, frame: string): string | null {
  return frame === painted ? null : redraw(frame)
}

// xterm moves its input textarea only when a write ends with the cursor
// elsewhere; a textarea left at the old font size widens the scroll area and
// pulls IME scrolling off the cursor. So moving it back writes this, then
// redraws the frame. It steps one row, not home: when the redraw lands on a
// later task, the textarea waits next to the cursor instead of at the top left,
// where a keystroke would scroll the view.
export function stepAway(cursorY: number): string {
  return `\x1b[${cursorY === 0 ? 2 : cursorY};1H`
}

export function TerminalView({ session, onBack }: { session: Session; onBack: () => void }) {
  const root = useRef<HTMLDivElement>(null)
  const container = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  const [font, setFont] = useState<FontChoice>(storedFont)
  const fontRef = useRef(font)
  // Last applied and fitted sizes, which the buttons step from.
  const sizes = useRef({ applied: 0, fit: 0 })
  const refit = useRef<() => void>(() => {})
  const follow = useRef<() => void>(() => {})
  const ime = useRef<{ reset(): void; dispose(): void } | null>(null)
  // A finger on the screen is reading or pinching; input must not pull the view away from it.
  const touching = useRef(false)
  const sender = useRef<Promise<unknown>>(Promise.resolve())
  const emit = (type: 'terminal_input' | 'interrupt', text: string): Promise<boolean> => {
    if (!touching.current) follow.current()
    const sent = sender.current.then(async () => {
      const response = await sendCommand(session, type, text, crypto.randomUUID())
      if (response.status === 'accepted') return true
      setError(response.status === 'delivery_unknown' ? '입력이 전달됐는지 확인하지 못했습니다. 다시 누르기 전에 화면을 확인하세요.' : '세션이 바뀌었거나 입력이 거부됐습니다.')
      return false
    }).catch(() => { setError('Terminal 연결이 끊겼습니다. agent는 계속 실행 중입니다.'); return false })
    sender.current = sent
    return sent
  }
  const hold = useRef<Hold | undefined>(undefined)
  const release = () => { hold.current?.release(); hold.current = undefined }
  const cancel = () => { hold.current?.cancel(); hold.current = undefined }
  const press = (event: PointerEvent<HTMLButtonElement>, text: string) => {
    if (!event.isPrimary || event.button !== 0) return
    cancel()
    event.currentTarget.setPointerCapture(event.pointerId)
    hold.current = startHold({ send: () => emit('terminal_input', text), delay: HOLD_DELAY_MS, interval: HOLD_INTERVAL_MS })
  }
  const arrow = (text: string) => ({
    onPointerDown: (event: PointerEvent<HTMLButtonElement>) => press(event, text),
    // Keyboard activation (Enter/Space) arrives as a click with detail 0; pointer clicks are handled above.
    onClick: (event: MouseEvent<HTMLButtonElement>) => { if (event.detail === 0) void emit('terminal_input', text) },
    onPointerUp: release,
    onPointerCancel: cancel,
    onLostPointerCapture: release,
    // Pointer capture delivers pointerup to the key even after the finger left
    // it, so a drag off the key must cancel instead of counting as a tap.
    onPointerMove: (event: PointerEvent<HTMLButtonElement>) => {
      if (!hold.current) return
      const r = event.currentTarget.getBoundingClientRect()
      if (event.clientX < r.left - DRAG_OFF_PX || event.clientX > r.right + DRAG_OFF_PX || event.clientY < r.top - DRAG_OFF_PX || event.clientY > r.bottom + DRAG_OFF_PX) cancel()
    },
    onContextMenu: (event: MouseEvent<HTMLButtonElement>) => event.preventDefault(),
  })
  useEffect(() => {
    const onVisibility = () => { if (document.visibilityState === 'hidden') cancel() }
    document.addEventListener('visibilitychange', onVisibility)
    return () => { document.removeEventListener('visibilitychange', onVisibility); cancel() }
  }, [session.id, session.runtime_binding])
  useEffect(() => { fontRef.current = font; refit.current(); storeFont(font) }, [font])
  useEffect(() => {
    const host = root.current
    if (!host) return
    let active = true
    const unmount = new AbortController()
    let term: import('@xterm/xterm').Terminal | undefined
    let timer: ReturnType<typeof setInterval> | undefined
    let input: { dispose(): void } | undefined
    let observer: ResizeObserver | undefined
    let recent: GridSize[] = []
    let painted: string | null = null
    let cursor = { x: 0, y: 0 }
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
      const fitted = fitFontSize({ availableWidth, cols: term.cols, cellRatio })
      const next = fontSizeFor(fontRef.current, fitted)
      sizes.current = { applied: current, fit: fitted }
      // Skip a sub-step grow when it already fits so calibration cannot ping-pong.
      const fits = !screen || screen.offsetWidth <= availableWidth
      if (fits && next > current && next - current < 0.5) return
      if (Math.abs(next - current) >= 0.25) {
        term.options.fontSize = next
        sizes.current.applied = next
        resync()
      }
    }
    // Moves xterm's input textarea back onto the cursor (stepAway). Queued from
    // the callback, the redraw is usually parsed in the same task as the step
    // away; xterm ends a parse loop after 12ms, so under load it waits for a
    // later task.
    const resync = () => {
      if (!term || painted === null) return
      term.write(stepAway(term.buffer.active.cursorY), () => { if (active && term && painted !== null) term.write(redraw(painted)) })
    }
    // For its copy and paste menu, xterm moves the textarea under a long press
    // or right click and leaves it there until the cursor moves, which a frame
    // ending where the last one did never does; the next keystroke would scroll
    // the view to it. It moves back on a later task, so the menu still opens
    // over the textarea.
    const onContextMenu = () => { setTimeout(resync) }
    refit.current = fit
    // A pinch anywhere on the screen scales the Terminal, not the page: the header
    // and key bar stay in place. The scale previews the gesture and the font size
    // is set on release, keeping the point between the fingers where it was.
    let pinch: PinchStart | undefined
    let distance = 0
    const span = (touches: TouchList) => Math.hypot(touches[0].clientX - touches[1].clientX, touches[0].clientY - touches[1].clientY)
    // Row height is not proportional to the font size, so each axis scales by
    // its own rendered cell size.
    const cell = (): Cell | undefined => {
      const screen = term?.element?.querySelector<HTMLElement>('.xterm-screen')
      if (!term || !screen || screen.offsetWidth <= 0 || screen.offsetHeight <= 0) return undefined
      return { width: screen.offsetWidth / term.cols, height: screen.offsetHeight / term.rows }
    }
    // xterm focuses its input with preventScroll, and the shell does not follow
    // the input when the keyboard shrinks it, so typing would happen off screen.
    // Input brings the cursor, at the start of the frame's last line, into view,
    // as the browser already does for IME typing. At a zoomed size this returns
    // to the line start, so the end of a line longer than the view is not
    // followed while typing.
    const reveal = () => {
      const shell = container.current
      const screen = term?.element?.querySelector<HTMLElement>('.xterm-screen')
      const size = cell()
      if (!term || !shell || !screen || !size) return
      const box = shell.getBoundingClientRect()
      const origin = screen.getBoundingClientRect()
      const left = origin.left - box.left - shell.clientLeft + shell.scrollLeft + cursor.x * size.width
      const top = origin.top - box.top - shell.clientTop + shell.scrollTop + cursor.y * size.height
      shell.scrollLeft = revealScroll(shell.scrollLeft, shell.clientWidth, left, size.width, size.width)
      shell.scrollTop = revealScroll(shell.scrollTop, shell.clientHeight, top, size.height, 2 * size.height)
    }
    follow.current = reveal
    const onTouchStart = (event: TouchEvent) => {
      const shell = container.current
      if (event.touches.length !== 2 || !term?.element || !shell) return
      const before = cell()
      if (!before) return
      const midX = (event.touches[0].clientX + event.touches[1].clientX) / 2
      const midY = (event.touches[0].clientY + event.touches[1].clientY) / 2
      const box = shell.getBoundingClientRect()
      const style = getComputedStyle(shell)
      const element = term.element.getBoundingClientRect()
      distance = span(event.touches)
      // Measured from the padding edge, where the scrolled content starts.
      pinch = {
        distance,
        font: term.options.fontSize ?? sizes.current.applied,
        cell: before,
        midX: midX - box.left - shell.clientLeft - parseFloat(style.paddingLeft),
        midY: midY - box.top - shell.clientTop - parseFloat(style.paddingTop),
        scrollLeft: shell.scrollLeft,
        scrollTop: shell.scrollTop,
      }
      term.element.style.transformOrigin = `${midX - element.left}px ${midY - element.top}px`
    }
    const onTouchMove = (event: TouchEvent) => {
      if (!pinch || event.touches.length !== 2 || !term?.element) return
      distance = span(event.touches)
      term.element.style.transform = `scale(${distance / pinch.distance})`
    }
    const onTouchEnd = (event: TouchEvent) => {
      if (!pinch || event.touches.length >= 2) return
      const start = pinch
      pinch = undefined
      if (term?.element) { term.element.style.transform = ''; term.element.style.transformOrigin = '' }
      const choice = pinchFont(start.font, start.distance, distance, sizes.current.fit)
      fontRef.current = choice
      fit()
      setFont(choice)
      requestAnimationFrame(() => requestAnimationFrame(() => {
        const shell = container.current
        const after = cell()
        if (!shell || !after) return
        shell.scrollLeft = (start.scrollLeft + start.midX) * after.width / start.cell.width - start.midX
        shell.scrollTop = (start.scrollTop + start.midY) * after.height / start.cell.height - start.midY
      }))
    }
    const onTouch = (event: TouchEvent) => { touching.current = event.touches.length > 0 }
    host.addEventListener('touchstart', onTouch, { passive: true })
    host.addEventListener('touchend', onTouch, { passive: true })
    host.addEventListener('touchcancel', onTouch, { passive: true })
    host.addEventListener('touchstart', onTouchStart, { passive: true })
    host.addEventListener('touchmove', onTouchMove, { passive: true })
    host.addEventListener('touchend', onTouchEnd, { passive: true })
    host.addEventListener('touchcancel', onTouchEnd, { passive: true })
    host.addEventListener('contextmenu', onContextMenu)
    void import('@xterm/xterm').then(({ Terminal }) => {
      if (!active || !container.current) return
      term = new Terminal({ cols: 120, rows: 40, convertEol: true, scrollback: 0, fontSize: 12, fontFamily: FONT_FAMILY, theme: { background: '#0a0c10', foreground: '#cdd1d6' } })
      term.open(container.current)
      input = term.onData(text => emit('terminal_input', text))
      ime.current = attachImeInput(term, text => { void emit('terminal_input', text) })
      let height = container.current.clientHeight
      // Growing cannot hide the cursor, and closing the keyboard keeps the input
      // focused, so only a shrink follows it; a reader stays put.
      observer = new ResizeObserver(() => {
        fit()
        const shell = container.current
        if (!shell || shell.clientHeight === height) return
        const shrunk = shell.clientHeight < height
        height = shell.clientHeight
        if (shrunk && document.activeElement === term?.textarea) reveal()
      })
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
          if (next.cols !== term.cols || next.rows !== term.rows) { term.resize(next.cols, next.rows); fit(); painted = null }
          const data = repaint(painted, frame.text)
          // A font change steps the live cursor away, so reveal reads where each
          // frame leaves it.
          if (data !== null) { term.write(data, () => { if (term) cursor = { x: term.buffer.active.cursorX, y: term.buffer.active.cursorY } }); painted = frame.text }
          setError('')
        } catch { if (active) setError('Terminal 화면을 읽을 수 없습니다. 목록에서 세션을 다시 여세요.') }
      })
      fit()
      void refresh()
      timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 1000)
    })
    return () => {
      active = false; unmount.abort(); refit.current = () => {}; follow.current = () => {}; if (timer) clearInterval(timer); observer?.disconnect(); input?.dispose(); ime.current?.dispose(); ime.current = null; term?.dispose()
      touching.current = false
      host.removeEventListener('touchstart', onTouch)
      host.removeEventListener('touchend', onTouch)
      host.removeEventListener('touchcancel', onTouch)
      host.removeEventListener('touchstart', onTouchStart)
      host.removeEventListener('touchmove', onTouchMove)
      host.removeEventListener('touchend', onTouchEnd)
      host.removeEventListener('touchcancel', onTouchEnd)
      host.removeEventListener('contextmenu', onContextMenu)
    }
  }, [session.id, session.runtime_binding])
  // The Terminal stays dark in both themes, like the pane it mirrors.
  return <div className={styles.screen} ref={root} data-scheme="dark">
    <TopBar
      onBack={onBack}
      backLabel="Chat으로 돌아가기"
      title={session.title || agentName(session.agent)}
      subtitle={<><span className={styles.label}>Terminal</span><span>PC 화면 크기 유지</span></>}
      actions={<div className={styles.zoom} role="group" aria-label="Terminal 글자 크기">
        <button type="button" className={ui.iconButton} onClick={() => setFont(stepFont(sizes.current.applied, sizes.current.fit, -1))} aria-label="글자 작게" disabled={font === 'fit'}><ZoomOut aria-hidden size={20} /></button>
        <button type="button" className={styles.fit} onClick={() => setFont('fit')} aria-label="화면 폭에 맞춤" aria-pressed={font === 'fit'}>맞춤</button>
        <button type="button" className={ui.iconButton} onClick={() => setFont(stepFont(sizes.current.applied, sizes.current.fit, 1))} aria-label="글자 크게" disabled={font !== 'fit' && font >= MAX_FONT_SIZE}><ZoomIn aria-hidden size={20} /></button>
      </div>}
    />
    {error && <div className={styles.notice}><Notice tone="warning">{error}</Notice></div>}
    <div className={styles.shell} ref={container} />
    {/* A tap would move focus to the button and close the soft keyboard the xterm input opened. */}
    <div className={styles.keys} role="group" aria-label="Terminal 특수 키" onMouseDown={event => event.preventDefault()}>
      <button type="button" className={styles.key} onClick={() => emit('terminal_input', '\x1b')}>Esc</button>
      <button type="button" className={styles.key} onClick={() => emit('terminal_input', '\t')}>Tab</button>
      <button type="button" className={styles.key} onClick={() => { ime.current?.reset(); void emit('interrupt', '') }}>Ctrl-C</button>
      <button type="button" className={styles.key} {...arrow('\x1b[A')} aria-label="위 화살표"><ArrowUp aria-hidden size={18} /></button>
      <button type="button" className={styles.key} {...arrow('\x1b[B')} aria-label="아래 화살표"><ArrowDown aria-hidden size={18} /></button>
      <button type="button" className={styles.key} onClick={() => { ime.current?.reset(); void emit('terminal_input', '\r') }} aria-label="Enter"><CornerDownLeft aria-hidden size={18} /></button>
    </div>
  </div>
}
