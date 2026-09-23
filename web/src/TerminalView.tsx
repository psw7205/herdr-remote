import { useEffect, useRef, useState } from 'react'
import { sendCommand, terminalFrame, type Session } from './api'
import '@xterm/xterm/css/xterm.css'

export function TerminalView({ session, onBack }: { session: Session; onBack: () => void }) {
  const container = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  const sender = useRef(Promise.resolve())
  const emit = (type: 'terminal_input' | 'interrupt', text: string) => {
    sender.current = sender.current.then(async () => {
      const response = await sendCommand(session, type, text, crypto.randomUUID())
      if (response.status !== 'accepted') setError(response.status === 'delivery_unknown' ? '입력 전달 여부가 불확실합니다. 다시 보내기 전에 PC 화면을 확인하세요.' : '세션이 바뀌었거나 입력이 거부됐습니다.')
    }).catch(() => setError('Terminal 연결이 끊겼습니다. agent는 계속 실행 중입니다.'))
  }
  useEffect(() => {
    if (!container.current) return
    let active = true
    let term: import('@xterm/xterm').Terminal | undefined
    let timer: ReturnType<typeof setInterval> | undefined
    let input: { dispose(): void } | undefined
    void import('@xterm/xterm').then(({ Terminal }) => {
      if (!active || !container.current) return
      term = new Terminal({ cols: 120, rows: 40, convertEol: true, scrollback: 0, fontSize: 12, theme: { background: '#0b1120', foreground: '#e7eef7' } })
      term.open(container.current)
      input = term.onData(text => emit('terminal_input', text))
      const refresh = async () => {
        try {
          const frame = await terminalFrame(session)
          if (active && term) { term.reset(); term.write(frame.text); setError('') }
        } catch { if (active) setError('Terminal 화면을 읽을 수 없습니다. 세션을 다시 선택하세요.') }
      }
      void refresh()
      timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 1000)
    })
    return () => { active = false; if (timer) clearInterval(timer); input?.dispose(); term?.dispose() }
  }, [session.id, session.runtime_binding])
  return <div className="session-layout terminal-layout">
    <header className="topbar"><button className="plain back" onClick={onBack} aria-label="Chat으로 돌아가기">‹</button><div className="session-heading"><strong>Terminal</strong><small>기존 Herdr pane · 화면 크기 유지</small></div></header>
    {error && <div className="notice" role="alert">{error}</div>}
    <div className="terminal-shell" ref={container} />
    <div className="terminal-keys" aria-label="Terminal 특수 키">
      <button onClick={() => emit('terminal_input', '\x1b')}>Esc</button><button onClick={() => emit('terminal_input', '\t')}>Tab</button><button onClick={() => emit('interrupt', '')}>Ctrl-C</button><button onClick={() => emit('terminal_input', '\x1b[A')}>↑</button><button onClick={() => emit('terminal_input', '\x1b[B')}>↓</button><button onClick={() => emit('terminal_input', '\r')}>Enter</button>
    </div>
  </div>
}
