import { useEffect, useRef, useState } from 'react'
import { listSessions, sessionCardText, supersededBy, type ConditionalInput, type Session } from './api'
import { SessionChat } from './SessionChat'
import { TerminalView } from './TerminalView'

function hashSession(): string | null {
  const match = location.hash.match(/^#session=(.+)$/)
  try { return match ? decodeURIComponent(match[1]) : null } catch { return null }
}
export function App() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [selected, setSelected] = useState<string | null>(hashSession)
  const [terminal, setTerminal] = useState<Session | null>(null)
  const [error, setError] = useState('')
  const [conditionalInput, setConditionalInput] = useState<ConditionalInput>('unknown')
  const open = useRef<string | null>(null)
  open.current = terminal?.id ?? selected
  useEffect(() => {
    let alive = true
    const refresh = async () => {
      try {
        const list = await listSessions()
        const from = open.current
        const successor = await supersededBy(from, list.sessions)
        if (!alive) return
        setSessions(list.sessions); setConditionalInput(list.conditionalInput); setError('')
        if (successor) {
          // Follow a pane: item re-keyed to its verified claude: session without
          // a hashchange, which would close the Terminal.
          if (hashSession() === from) history.replaceState(null, '', `#session=${encodeURIComponent(successor.id)}`)
          setSelected(current => current === from ? successor.id : current)
          setTerminal(current => current?.id === from ? (successor.terminal ? successor : null) : current)
        }
      } catch { if (alive) setError('Herdr 연결을 확인할 수 없습니다. 잠시 후 다시 시도합니다.') }
    }
    void refresh()
    const timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 3000)
    const visible = () => { if (document.visibilityState === 'visible') void refresh() }
    document.addEventListener('visibilitychange', visible)
    return () => { alive = false; clearInterval(timer); document.removeEventListener('visibilitychange', visible) }
  }, [])
  useEffect(() => {
    const changed = () => { setSelected(hashSession()); setTerminal(null) }
    window.addEventListener('hashchange', changed)
    return () => window.removeEventListener('hashchange', changed)
  }, [])
  const session = selected ? sessions.find(item => item.id === selected) ?? terminal : null
  if (terminal) return <TerminalView key={`${terminal.id}:${terminal.runtime_binding}`} session={terminal} onBack={() => setTerminal(null)} />
  if (selected && session) return <SessionChat key={selected} initial={session} onBack={() => { location.hash = ''; setSelected(null) }} onTerminal={setTerminal} />
  return <div className="home-layout">
    <header className="home-header"><span className="mark">H<span>·</span></span><div><p className="eyebrow">HERDR MOBILE</p><h1>진행 중인 대화</h1></div></header>
    {error && <div className="notice" role="alert">{error}</div>}
    {conditionalInput === 'unsupported' && <div className="notice" role="status">현재 Herdr에 conditional input API가 없어 Chat 입력과 Terminal을 사용할 수 없습니다. agent는 계속 실행 중입니다. PC에서 Herdr patch 적용 여부를 확인하세요.</div>}
    <main className="session-list">
      {sessions.length === 0 && !error && <div className="empty-state"><strong>실행 중인 agent가 없습니다.</strong><p>PC의 Herdr에서 agent를 실행하면 이곳에 표시됩니다.</p></div>}
      {sessions.map(item => <button type="button" className="session-card" key={item.id} onClick={() => { location.hash = `session=${encodeURIComponent(item.id)}`; setSelected(item.id) }}>
        <span className="avatar">{item.agent === 'claude' ? 'C' : item.agent.slice(0, 1).toUpperCase()}</span>
        <span className="session-card-body"><strong>{item.title || (item.agent === 'claude' ? 'Claude Code' : item.agent)}</strong><small>{item.agent === 'claude' ? 'Claude Code' : item.agent} · {item.project.split('/').filter(Boolean).at(-1) ?? item.project} · {item.pane_id}</small><span>{sessionCardText(item)}</span></span>
        <span className={`status-dot ${item.status}`} aria-label={item.status} />
      </button>)}
    </main>
    <footer>Herdr의 기존 세션과 연결됩니다.</footer>
  </div>
}
