import { useCallback, useEffect, useRef, useState } from 'react'
import { MessageMarkdown } from './MessageMarkdown'
import { chooseCommand, readPending, type PendingCommand } from './commandDelivery'
import { eventURL, getSession, sendCommand, type Cursor, type Event, type Message, type Session } from './api'

const statusLabels: Record<Session['status'], string> = {
  needs_attention: '입력 필요', working: '작업 중', idle: '대기 중', completed: '응답 완료', error: '상태 확인 필요',
}

export function SessionChat({ initial, onBack, onTerminal }: { initial: Session; onBack: () => void; onTerminal: (session: Session) => void }) {
  const [session, setSession] = useState(initial)
  const [messages, setMessages] = useState<Message[]>([])
  const [connection, setConnection] = useState<'syncing' | 'connected' | 'disconnected'>('syncing')
  const [draft, setDraft] = useState(() => sessionStorage.getItem(`draft:${initial.id}`) ?? '')
  const [pending, setPending] = useState<PendingCommand | null>(() => readPending(`command:${initial.id}`))
  const [delivery, setDelivery] = useState(() => readPending(`command:${initial.id}`) ? '이전 입력의 전달 여부가 불확실합니다. 같은 초안은 기존 command ID로만 확인합니다.' : '')
  const [sending, setSending] = useState(false)
  const [unsupported, setUnsupported] = useState(false)
  const cursor = useRef<Cursor | null>(null)
  const bottom = useRef<HTMLDivElement>(null)
  const socket = useRef<WebSocket | null>(null)

  useEffect(() => { sessionStorage.setItem(`draft:${session.id}`, draft) }, [draft, session.id])
  useEffect(() => { bottom.current?.scrollIntoView({ behavior: 'smooth' }) }, [messages.length])

  useEffect(() => {
    let alive = true
    let reconnect: ReturnType<typeof setTimeout> | undefined
    let ws: WebSocket | undefined
    const load = async () => {
      const data = await getSession(initial.id)
      if (!alive) return
      setSession(data.session)
      setMessages(Array.isArray(data.snapshot.data) ? data.snapshot.data : [])
      cursor.current = data.snapshot.cursor
    }
    const connect = async () => {
      try {
        setConnection('syncing')
        await load()
        if (!alive || !cursor.current) return
        ws = new WebSocket(eventURL(initial.id, cursor.current))
        socket.current = ws
        ws.onopen = () => { if (alive) setConnection('connected') }
        ws.onmessage = message => {
          if (!alive) return
          try {
            const event = JSON.parse(message.data) as Event
            if (event.type === 'session.snapshot') {
              const snapshot = event.payload as { cursor: Cursor; data: Message[] }
              cursor.current = snapshot.cursor
              setMessages(Array.isArray(snapshot.data) ? snapshot.data : [])
              setUnsupported(false)
              return
            }
            const last = cursor.current
            if (!last || last.epoch !== event.cursor.epoch || event.cursor.sequence !== last.sequence + 1) {
              if (last?.epoch === event.cursor.epoch && event.cursor.sequence <= last.sequence) return
              ws?.close()
              return
            }
            cursor.current = event.cursor
            if (event.type === 'message.user' || event.type === 'message.assistant') {
              const next = event.payload as Message
              setMessages(existing => existing.some(item => item.id === next.id) ? existing : [...existing, next])
            } else if (event.type === 'session.error') {
              setUnsupported(true)
            }
          } catch { ws?.close() }
        }
        ws.onclose = () => {
          if (!alive) return
          setConnection('disconnected')
          reconnect = setTimeout(connect, 1200)
        }
      } catch {
        if (!alive) return
        setConnection('disconnected')
        reconnect = setTimeout(connect, 2000)
      }
    }
    const visible = () => { if (document.visibilityState === 'visible') ws?.close() }
    document.addEventListener('visibilitychange', visible)
    void connect()
    const metadata = setInterval(() => { void getSession(initial.id).then(data => { if (alive) setSession(data.session) }).catch(() => {}) }, 3000)
    return () => { alive = false; clearTimeout(reconnect); clearInterval(metadata); document.removeEventListener('visibilitychange', visible); ws?.close(); socket.current = null }
  }, [initial.id])

  const clearPending = () => {
    sessionStorage.removeItem(`command:${session.id}`)
    setPending(null)
  }
  const send = useCallback(async () => {
    const text = draft.trim()
    if (!text || sending || !session.active || !session.chat || !session.runtime_binding || !['idle', 'completed'].includes(session.status)) return
    const command = chooseCommand(pending, session.runtime_binding, text, () => crypto.randomUUID())
    if (!command) { setDelivery('이전 입력의 전달 여부를 Terminal에서 확인한 뒤 새 입력을 작성하세요.'); return }
    sessionStorage.setItem(`command:${session.id}`, JSON.stringify(command))
    setPending(command)
    setSending(true); setDelivery('전달 상태 확인 중…')
    try {
      const result = await sendCommand(session, 'prompt', text, command.id)
      if (result.status === 'accepted') {
        sessionStorage.removeItem(`command:${session.id}`)
        setPending(null); setDraft(''); setDelivery('입력 전달됨 · 대화는 native transcript 기준')
      } else if (result.status === 'delivery_unknown') {
        setDelivery('전달 여부가 불확실합니다. 같은 입력은 기존 command ID로만 확인합니다.')
      } else {
        sessionStorage.removeItem(`command:${session.id}`)
        setPending(null)
        setDelivery(result.code === 'SESSION_CHANGED' || result.code === 'RUNTIME_BINDING_MISMATCH' ? '세션이 바뀌었습니다. 목록에서 다시 선택하세요.' : `전달 거부: ${result.code ?? '확인 필요'}`)
      }
    } catch { setDelivery('연결이 끊겼습니다. 같은 입력은 기존 command ID로만 확인합니다.') }
    finally { setSending(false) }
  }, [draft, sending, session, pending])

  const interrupt = async () => {
    if (!session.runtime_binding) return
    const result = await sendCommand(session, 'interrupt', '', crypto.randomUUID()).catch(() => ({ status: 'delivery_unknown' as const }))
    setDelivery(result.status === 'accepted' ? '중단 입력을 전달했습니다.' : '중단 전달 여부를 확인할 수 없습니다.')
  }
  const canPrompt = session.active && session.chat && Boolean(session.runtime_binding) && ['idle', 'completed'].includes(session.status)
  return <div className="session-layout">
    <header className="topbar">
      <button type="button" className="plain back" onClick={onBack} aria-label="세션 목록">‹</button>
      <div className="session-heading"><strong>{session.title || (session.agent === 'claude' ? 'Claude Code' : session.agent)}</strong><small>{session.agent === 'claude' ? 'Claude Code' : session.agent} · {session.project.split('/').filter(Boolean).at(-1) ?? session.project} · {session.pane_id}</small></div>
      <span className={`status-pill ${session.status}`}>{statusLabels[session.status]}</span>
      {session.terminal && <button type="button" className="outline" onClick={() => onTerminal(session)}>Terminal</button>}
    </header>
    <div className={`connection ${connection}`}>{connection === 'connected' ? '연결됨' : connection === 'syncing' ? '대화 동기화 중…' : '연결 끊김 · agent는 계속 실행 중'}</div>
    <main className="conversation" aria-live="polite">
      {!session.chat && <div className="notice">{session.terminal ? '이 세션의 대화를 안전하게 찾지 못했습니다. Terminal에서 확인하세요.' : '이 세션의 대화를 안전하게 찾지 못했습니다. PC의 Herdr에서 확인하세요.'}</div>}
      {unsupported && <div className="notice">{session.terminal ? 'Chat에서 표현할 수 없는 내용이 있습니다. Terminal을 열어 확인하세요.' : 'Chat에서 표현할 수 없는 내용이 있습니다. PC의 Herdr에서 확인하세요.'}</div>}
      {messages.map(message => <article className={`message ${message.role}`} key={message.id}>
        <div className="message-label">{message.role === 'user' ? '나' : 'Claude'}</div>
        <div className="markdown"><MessageMarkdown text={message.text} /></div>
      </article>)}
      {messages.length === 0 && session.chat && <p className="empty-chat">아직 표시할 대화가 없습니다.</p>}
      <div ref={bottom} />
    </main>
    <form className="composer" onSubmit={event => { event.preventDefault(); void send() }}>
      {delivery && <p className="delivery" role="status">{delivery}</p>}
      {pending && !sending && <button type="button" className="outline" onClick={() => { clearPending(); setDraft(''); setDelivery('Terminal에서 이전 입력을 확인한 후 새 입력을 작성할 수 있습니다.') }}>Terminal 확인 후 새 입력</button>}
      {!canPrompt && <p className="composer-hint">{!session.active ? '종료된 세션에는 입력할 수 없습니다.' : session.terminal ? '이 상태의 입력은 Terminal에서 진행하세요.' : '이 상태의 입력은 PC의 Herdr에서 진행하세요.'}</p>}
      <textarea aria-label="메시지" placeholder="이어서 요청하기" value={draft} onChange={event => setDraft(event.target.value)} disabled={!canPrompt || sending} rows={2} />
      <div className="composer-actions"><button type="button" className="outline" onClick={() => void interrupt()} disabled={!session.active || !session.runtime_binding}>중단</button><button type="submit" disabled={!canPrompt || sending || !draft.trim()}>{sending ? '전달 중…' : '보내기'}</button></div>
    </form>
  </div>
}
