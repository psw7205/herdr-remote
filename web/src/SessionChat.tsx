import { Fragment, memo, useCallback, useEffect, useRef, useState } from 'react'
import { ArrowDown, FileDiff, RefreshCw, SquareTerminal, TextQuote, WifiOff } from 'lucide-react'
import { chatSelection } from './chatSelection'
import { CopyButton, MessageMarkdown, renderedText } from './MessageMarkdown'
import { applyMessageEvent, chatEntries, entryEnd, entryKey, textCount, toolsLive, turnSummaries, type TurnSummary } from './chatItems'
import { chooseCommand, pendingCommandCopy, readPending, rejectedMessage, type PendingCommand } from './commandDelivery'
import { chatUnavailableNotice, composerHint, eventURL, getChanges, getSession, lifecycleNotice, sendCommand, sessionLifecycle, type Cursor, type Event, type Message, type Session } from './api'
import { Composer, type ComposerStatus } from './Composer'
import { appendQuote, readDraft, writeDraft } from './draftStore'
import { Notice, type NoticeAction } from './Notice'
import { agentName, projectName, statusPresentation } from './presentation'
import { CLOSE_RETRY_MS, createReconnector } from './reconnect'
import { StatusBadge } from './StatusBadge'
import { dividerLabel, durationLabel } from './time'
import { ToolGroup } from './ToolActivity'
import { TimeoutError, withTimeout } from './withTimeout'
import { TopBar } from './TopBar'
import { useStickToBottom } from './useStickToBottom'
import styles from './SessionChat.module.css'
import ui from './ui.module.css'

type DeliveryStatus = ComposerStatus & { kind?: 'accepted' | 'pending' }
const UNCERTAIN = '같은 내용을 다시 보내면 중복 없이 확인합니다.'
const ACCEPTED_NOTICE_MS = 8000
// A snapshot fetch stalled by a suspend or network switch would otherwise hold
// the reconnector in `connecting`, where wake events are no-ops; failing it
// lets the 2s retry take over. The limit doubles after each timeout so a large
// snapshot on a slow link still completes instead of restarting forever.
const SNAPSHOT_TIMEOUT_MS = 15000
const SNAPSHOT_TIMEOUT_MAX_MS = 120000
const BRANCH_REFRESH_MS = 30000
const SELECTION_SETTLE_MS = 250

// Memoized so typing in the composer or a metadata poll does not re-render
// (and re-parse the Markdown of) a long conversation. Tool calls belong to the
// agent's side of a turn.
const MessageList = memo(function MessageList({ messages, agent, live }: { messages: Message[]; agent: string; live: boolean }) {
  const entries = chatEntries(messages)
  const summaries = turnSummaries(entries, live)
  return entries.map((entry, index) => {
    const previous = entries[index - 1]
    const timestamp = entry.kind === 'tools' ? entry.tools[0].timestamp : entry.message.timestamp
    const label = dividerLabel(previous ? entryEnd(previous) : null, timestamp)
    const role = entry.kind === 'tools' ? 'assistant' : entry.message.role
    const turn = previous && (previous.kind === 'tools' ? 'assistant' : previous.message.role) !== role ? '' : undefined
    const divider = label && <div className={styles.divider} role="separator"><time dateTime={timestamp}>{label}</time></div>
    const summary = summaries.get(entryKey(entry))
    const footer = summary && <TurnFooter summary={summary} />
    if (entry.kind === 'tools') return <Fragment key={entry.id}>{divider}<div className={styles.tools} data-turn={turn}><ToolGroup tools={entry.tools} live={live} /></div>{footer}</Fragment>
    const { message } = entry
    return <Fragment key={message.id}>
      {divider}
      <article className={message.role === 'user' ? styles.user : styles.assistant} data-turn={turn}>
        <span className="visually-hidden">{message.role === 'user' ? '나' : agent}</span>
        {message.role === 'user' ? <div className={styles.bubble}><MessageMarkdown text={message.text} /></div> : <MessageMarkdown text={message.text} id={answerID(message.id)} />}
      </article>
      {footer}
    </Fragment>
  })
})

const answerID = (id: string) => `answer-${id}`

export function TurnFooter({ summary }: { summary: TurnSummary }) {
  const { answer, duration } = summary
  const plain = () => {
    if (!answer) return ''
    const prose = document.getElementById(answerID(answer.id))
    return (prose && renderedText(prose)) || answer.text
  }
  return <div className={styles.turnEnd}>
    {answer && <span className={styles.turnActions}>
      <CopyButton label="Markdown" description="답변을 Markdown으로 복사" read={() => answer.text} />
      <CopyButton label="텍스트" description="답변을 텍스트로 복사" read={plain} />
    </span>}
    {duration !== undefined && <span className={styles.duration}>작업 시간 {durationLabel(duration)}</span>}
  </div>
}

export function SessionChat({ initial, onBack, onTerminal, onChanges }: { initial: Session; onBack: () => void; onTerminal: (session: Session) => void; onChanges?: (session: Session) => void }) {
  const [session, setSession] = useState(initial)
  const [messages, setMessages] = useState<Message[]>([])
  const [loaded, setLoaded] = useState(false)
  const [connection, setConnection] = useState<'syncing' | 'connected' | 'disconnected'>('syncing')
  const [draft, setDraft] = useState(() => readPending(`command:${initial.id}`)?.text ?? readDraft(initial.id))
  const [pending, setPending] = useState<PendingCommand | null>(() => readPending(`command:${initial.id}`))
  const [delivery, setDelivery] = useState<DeliveryStatus | null>(() => readPending(`command:${initial.id}`) ? { tone: 'warning', text: `이전 입력이 전달됐는지 확인하지 못했습니다. ${UNCERTAIN}`, kind: 'pending' } : null)
  const [sending, setSending] = useState(false)
  const [unsupported, setUnsupported] = useState(false)
  const [branch, setBranch] = useState<string | undefined>(undefined)
  const [selected, setSelected] = useState<string | null>(null)
  const cursor = useRef<Cursor | null>(null)
  const scroller = useRef<HTMLElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const socket = useRef<WebSocket | null>(null)
  // Message count when a prompt was accepted; its transcript echo clears the
  // notice. A slash command leaves no visible echo, so a timer clears it too.
  const echoAfter = useRef<number | null>(null)
  const acceptedTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  // Tool calls are not counted as new messages; a pinned reader still follows them.
  const texts = textCount(messages)
  const { away, unseen, toBottom } = useStickToBottom(scroller, content, texts)

  // While delivery is uncertain the draft lives only in the pending command
  // (sessionStorage), so a device restart cannot offer the same prompt again
  // under a new command_id after the receipt reference is gone.
  useEffect(() => { writeDraft(session.id, pending ? '' : draft) }, [draft, session.id, pending])
  useEffect(() => {
    if (echoAfter.current === null || texts <= echoAfter.current) return
    echoAfter.current = null
    setDelivery(current => current?.kind === 'accepted' ? null : current)
  }, [texts])
  useEffect(() => () => clearTimeout(acceptedTimer.current), [])

  useEffect(() => {
    let alive = true
    let ws: WebSocket | undefined
    // A reconnect this client started on returning to the foreground is not a lost connection.
    let resuming = false
    let snapshotTimeout = SNAPSHOT_TIMEOUT_MS
    const load = async () => {
      let data
      try {
        data = await withTimeout(signal => getSession(initial.id, signal), snapshotTimeout)
      } catch (error) {
        if (error instanceof TimeoutError) snapshotTimeout = Math.min(snapshotTimeout * 2, SNAPSHOT_TIMEOUT_MAX_MS)
        throw error
      }
      snapshotTimeout = SNAPSHOT_TIMEOUT_MS
      if (!alive) return
      setSession(data.session)
      setMessages(Array.isArray(data.snapshot.data) ? data.snapshot.data : [])
      cursor.current = data.snapshot.cursor
      setLoaded(true)
    }
    const connect = async () => {
      try {
        setConnection('syncing')
        await load()
        if (!alive || !cursor.current) return
        ws = new WebSocket(eventURL(initial.id, cursor.current))
        socket.current = ws
        ws.onopen = () => { if (alive) { reconnect.connected(); setConnection('connected') } }
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
            if (event.type.startsWith('message.')) {
              setMessages(existing => applyMessageEvent(existing, event.type, event.payload) ?? existing)
            } else if (event.type === 'session.error') {
              setUnsupported(true)
            }
          } catch { ws?.close() }
        }
        ws.onclose = () => {
          if (!alive) return
          const forced = resuming
          resuming = false
          setConnection(forced ? 'syncing' : 'disconnected')
          reconnect.schedule(forced ? 0 : CLOSE_RETRY_MS)
        }
      } catch (error) {
        if (alive) setConnection('disconnected')
        throw error
      }
    }
    const reconnect = createReconnector(connect)
    // A socket that was open or still shaking hands when the device slept or
    // changed networks may be dead without the browser noticing for minutes, so
    // the foreground return closes it and reconnects at once. A network return
    // closes only a pending handshake; an open socket is left alone.
    const closeStale = (openToo: boolean) => {
      const stale = ws && (ws.readyState === WebSocket.CONNECTING || (openToo && ws.readyState === WebSocket.OPEN))
      if (stale) { resuming = true; ws?.close() } else reconnect.wake()
    }
    const visible = () => { if (document.visibilityState === 'visible') closeStale(true) }
    const online = () => closeStale(false)
    const wake = () => reconnect.wake()
    document.addEventListener('visibilitychange', visible)
    window.addEventListener('pageshow', wake)
    window.addEventListener('online', online)
    void reconnect.start()
    const metadata = setInterval(() => { void getSession(initial.id).then(data => { if (alive) setSession(data.session) }).catch(() => {}) }, 3000)
    return () => {
      alive = false
      reconnect.cancel()
      clearInterval(metadata)
      document.removeEventListener('visibilitychange', visible)
      window.removeEventListener('pageshow', wake)
      window.removeEventListener('online', online)
      ws?.close()
      socket.current = null
    }
  }, [initial.id])

  // Each listing runs git on the host, so the branch is read when the chat
  // opens and on a later return to the page, never on the status poll.
  const hasProject = Boolean(session.project)
  useEffect(() => {
    if (!hasProject) return
    let request: AbortController | null = null
    let last = 0
    const load = () => {
      if (request) return
      const current = new AbortController()
      request = current
      last = Date.now()
      getChanges(initial.id, current.signal)
        .then(changes => { if (!current.signal.aborted) setBranch(changes.repository ? changes.branch : undefined) })
        .catch(() => { if (!current.signal.aborted) setBranch(undefined) })
        .finally(() => { if (request === current) request = null })
    }
    const visible = () => { if (document.visibilityState === 'visible' && Date.now() - last >= BRANCH_REFRESH_MS) load() }
    load()
    document.addEventListener('visibilitychange', visible)
    return () => {
      document.removeEventListener('visibilitychange', visible)
      request?.abort()
    }
  }, [initial.id, hasProject])

  // iOS fires selectionchange continuously while a handle is dragged, and a tap
  // on the quote button collapses the selection before its click lands; the
  // delay keeps the button and its text until the click has run.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const read = () => {
      clearTimeout(timer)
      timer = setTimeout(() => setSelected(chatSelection(document.getSelection(), content.current)), SELECTION_SETTLE_MS)
    }
    document.addEventListener('selectionchange', read)
    return () => {
      document.removeEventListener('selectionchange', read)
      clearTimeout(timer)
    }
  }, [])

  const clearPending = () => {
    sessionStorage.removeItem(`command:${session.id}`)
    setPending(null)
  }
  const pendingCopy = pendingCommandCopy(session.terminal)
  const send = useCallback(async () => {
    const text = draft.trim()
    if (!text || sending || !session.active || !session.chat || !session.runtime_binding || !['idle', 'completed'].includes(session.status)) return
    const command = chooseCommand(pending, session.runtime_binding, text, () => crypto.randomUUID())
    if (!command) { setDelivery({ tone: 'warning', text: pendingCopy.blocked, kind: 'pending' }); return }
    sessionStorage.setItem(`command:${session.id}`, JSON.stringify(command))
    setPending(command)
    setSending(true); setDelivery({ tone: 'progress', text: '전달하는 중…' })
    try {
      const result = await sendCommand(session, 'prompt', text, command.id)
      if (result.status === 'accepted') {
        sessionStorage.removeItem(`command:${session.id}`)
        setPending(null); setDraft('')
        echoAfter.current = texts
        setDelivery({ tone: 'success', text: '전달했습니다. 응답을 기다리는 중…', kind: 'accepted' })
        clearTimeout(acceptedTimer.current)
        acceptedTimer.current = setTimeout(() => setDelivery(current => current?.kind === 'accepted' ? null : current), ACCEPTED_NOTICE_MS)
        toBottom()
      } else if (result.status === 'delivery_unknown') {
        setDelivery({ tone: 'warning', text: `전달됐는지 확인하지 못했습니다. ${UNCERTAIN}`, kind: 'pending' })
      } else {
        sessionStorage.removeItem(`command:${session.id}`)
        setPending(null)
        setDelivery({ tone: 'danger', text: rejectedMessage(result.code) })
      }
    } catch { setDelivery({ tone: 'warning', text: `연결이 끊겨 전달됐는지 알 수 없습니다. ${UNCERTAIN}`, kind: 'pending' }) }
    finally { setSending(false) }
  }, [draft, sending, session, pending, pendingCopy.blocked, texts, toBottom])

  const interrupt = async () => {
    if (!session.runtime_binding) return
    const result = await sendCommand(session, 'interrupt', '', crypto.randomUUID()).catch(() => ({ status: 'delivery_unknown' as const }))
    setDelivery(result.status === 'accepted' ? { tone: 'neutral', text: '중단을 요청했습니다.' } : { tone: 'warning', text: '중단 요청이 전달됐는지 확인하지 못했습니다.' })
  }

  const openTerminal = () => onTerminal(session)
  const terminalAction: NoticeAction[] = session.terminal ? [{ label: 'Terminal 열기', onClick: openTerminal }] : []
  const lifecycle = sessionLifecycle(session)
  const controllable = lifecycle === 'active' && session.active && session.chat && Boolean(session.runtime_binding)
  const canPrompt = controllable && ['idle', 'completed'].includes(session.status)
  // While delivery is uncertain the draft is the pending command's text.
  const quote = controllable && !pending && !sending ? selected : null
  const quoteSelection = () => {
    if (!quote) return
    setDraft(current => appendQuote(current, quote))
    document.getSelection()?.removeAllRanges()
    setSelected(null)
  }
  // Stop stays available beside whichever status line is showing.
  const stopAction: NoticeAction[] = controllable && session.status === 'working' ? [{ label: '작업 중단', onClick: () => void interrupt() }] : []
  const pendingActions: NoticeAction[] = pending && !sending ? [...terminalAction, { label: pendingCopy.action, onClick: () => { clearPending(); setDraft(''); setDelivery({ tone: 'neutral', text: pendingCopy.cleared }) } }] : []
  const status: ComposerStatus | null = delivery
    ? { ...delivery, actions: [...pendingActions, ...stopAction] }
    : !canPrompt ? { tone: session.status === 'needs_attention' ? 'warning' : session.status === 'working' && controllable ? 'working' : 'neutral', text: composerHint(session), actions: [...(session.status === 'needs_attention' ? terminalAction : []), ...stopAction] }
    : null

  const agent = agentName(session.agent)
  return <div className={styles.screen}>
    <TopBar
      onBack={onBack}
      backLabel="세션 목록"
      title={session.title || agent}
      subtitle={<><StatusBadge status={statusPresentation(session)} /><span>{projectName(session.project)}</span></>}
      actions={<>
        {onChanges && session.project && <button type="button" className={ui.iconButton} onClick={() => onChanges(session)} aria-label="변경된 파일" title="변경된 파일"><FileDiff aria-hidden size={22} /></button>}
        {session.terminal && <button type="button" className={ui.iconButton} onClick={openTerminal} aria-label="Terminal 열기" title="Terminal"><SquareTerminal aria-hidden size={22} /></button>}
      </>}
    />
    {connection === 'disconnected' && <div className={styles.band} data-state="lost" role="status"><WifiOff aria-hidden size={16} />연결이 끊겼습니다. agent는 계속 실행 중이며 다시 연결하고 있습니다.</div>}
    {connection === 'syncing' && loaded && <div className={styles.band} data-state="syncing" role="status"><RefreshCw aria-hidden size={16} />다시 연결하는 중…</div>}
    {(lifecycleNotice(session) || !session.chat || unsupported) && <div className={styles.notices}>
      {lifecycleNotice(session) && <Notice tone="warning">{lifecycleNotice(session)}</Notice>}
      {!session.chat && <Notice tone="info" actions={terminalAction}>{chatUnavailableNotice(session)}</Notice>}
      {unsupported && <Notice tone="warning" actions={terminalAction}>{session.terminal ? 'Chat에 표시할 수 없는 내용이 있습니다. Terminal에서 확인하세요.' : 'Chat에 표시할 수 없는 내용이 있습니다. PC의 Herdr에서 확인하세요.'}</Notice>}
    </div>}
    <div className={styles.frame}>
      <main className={styles.scroller} ref={scroller} aria-label="대화" aria-busy={!loaded}>
        <div className={styles.content} ref={content}>
          {!loaded && session.chat && <div className={styles.skeleton} aria-hidden="true"><span data-role="user" /><span /><span /><span data-short="" /></div>}
          <MessageList messages={messages} agent={agent} live={toolsLive(session)} />
          {loaded && messages.length === 0 && session.chat && <p className={styles.empty}>아직 대화가 없습니다.</p>}
        </div>
      </main>
      {quote
        ? <button type="button" className={styles.jump} onPointerDown={event => event.preventDefault()} onClick={quoteSelection}><TextQuote aria-hidden size={16} />선택한 글 인용</button>
        : away && <button type="button" className={styles.jump} onClick={() => toBottom(true)}><ArrowDown aria-hidden size={16} />{unseen > 0 ? `새 메시지 ${unseen}개` : '최신으로'}</button>}
    </div>
    <Composer draft={draft} onDraft={setDraft} canType={controllable} canSend={canPrompt} sending={sending} onSend={() => void send()} status={status} place={session.project ? { folder: projectName(session.project), branch } : null} />
  </div>
}
