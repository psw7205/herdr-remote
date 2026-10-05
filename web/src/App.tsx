import { useCallback, useEffect, useRef, useState } from 'react'
import { listSessions, noStart, supersededBy, type BridgeBuild, type ConditionalInput, type Session, type StartCapability } from './api'
import { loadedClientBuild } from './build'
import { backAction, followAlias, formatRoute, parseRoute, resolveTerminal, startRoute, successorRoute, type Route } from './route'
import { ChangedFiles, FileDiffView } from './ChangesView'
import { SessionChat } from './SessionChat'
import { SessionList } from './SessionList'
import { StartSession } from './StartSession'
import { TerminalView } from './TerminalView'
import { useVisualViewportShell } from './viewport'

const loadedBuild = loadedClientBuild(typeof document === 'undefined' ? null : document)

// History entries this app pushed above the current one (0 for a deep link).
function historyDepth(): number {
  const state: unknown = history.state
  return state && typeof state === 'object' && 'herdrDepth' in state && typeof state.herdrDepth === 'number' ? state.herdrDepth : 0
}
function writeRoute(route: Route, replace: boolean) {
  const url = `${location.pathname}${location.search}${formatRoute(route)}`
  if (replace) history.replaceState(history.state, '', url)
  else history.pushState({ herdrDepth: historyDepth() + 1 }, '', url)
}

export function App() {
  const shell = useRef<HTMLDivElement>(null)
  useVisualViewportShell(shell)
  const [sessions, setSessions] = useState<Session[]>([])
  const [loaded, setLoaded] = useState(false)
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash))
  // The Terminal keeps the binding it opened with. A later binding under the
  // same id is not followed silently: for a pane: item it can be another process.
  const [terminal, setTerminal] = useState<Session | null>(null)
  const [error, setError] = useState('')
  const [conditionalInput, setConditionalInput] = useState<ConditionalInput>('unknown')
  const [start, setStart] = useState<StartCapability>(noStart)
  const [bridge, setBridge] = useState<BridgeBuild | null>(null)
  const open = useRef<string | null>(null)
  const ended = useRef(new Set<string>())
  // Superseded pane: ids mapped to their successors, for older history entries.
  const aliases = useRef(new Map<string, string>())
  open.current = route.session

  const navigate = useCallback((next: Route, replace = false) => { writeRoute(next, replace); setRoute(next) }, [])
  useEffect(() => {
    const sync = () => {
      const parsed = parseRoute(location.hash)
      const next = followAlias(parsed, aliases.current)
      if (next !== parsed) writeRoute(next, true)
      setRoute(next)
    }
    window.addEventListener('popstate', sync)
    window.addEventListener('hashchange', sync)
    return () => { window.removeEventListener('popstate', sync); window.removeEventListener('hashchange', sync) }
  }, [])
  useEffect(() => {
    let alive = true
    const refresh = async () => {
      try {
        const list = await listSessions()
        const from = open.current
        const successor = await supersededBy(from, list.sessions, ended.current)
        if (!alive) return
        setSessions(list.sessions); setConditionalInput(list.conditionalInput); setStart(list.start); setBridge(list.bridge); setError(''); setLoaded(true)
        if (successor) {
          // Follow a pane: item re-keyed to its identified claude: or codex:
          // session, with the successor's own binding (if any) from the list.
          // Replacing the entry keeps Back pointing where it pointed before.
          if (from) aliases.current.set(from, successor.id)
          const current = parseRoute(location.hash)
          if (current.session === from) {
            const next = successorRoute(current, successor)
            writeRoute(next, true)
            setRoute(next)
          }
          setTerminal(target => target?.id === from ? (successor.terminal ? successor : null) : target)
        }
      } catch { if (alive) { setError('Herdr에 연결할 수 없습니다. 잠시 후 자동으로 다시 시도합니다.'); setLoaded(true) } }
    }
    void refresh()
    const timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 3000)
    const visible = () => { if (document.visibilityState === 'visible') void refresh() }
    document.addEventListener('visibilitychange', visible)
    return () => { alive = false; clearInterval(timer); document.removeEventListener('visibilitychange', visible) }
  }, [])

  const listed = route.session ? sessions.find(item => item.id === route.session) : undefined
  // Decided during render so a reload on the Terminal never mounts the Chat
  // first. The effect keeps that binding and drops it when the Terminal is left.
  const terminalTarget = resolveTerminal(route, terminal, listed)
  useEffect(() => { setTerminal(terminalTarget) }, [terminalTarget])
  // A Terminal route whose session has no Terminal now (a binding lost across a
  // reload) shows the Chat and says so in the URL, instead of switching later.
  const fallbackToChat = route.view === 'terminal' && !terminalTarget && loaded && Boolean(listed)
  useEffect(() => {
    if (fallbackToChat && route.session) navigate({ session: route.session, view: 'chat' }, true)
  }, [fallbackToChat, route.session, navigate])

  const back = useCallback(() => {
    const action = backAction(route, historyDepth())
    if (action.kind === 'history') history.back()
    else navigate(action.route, true)
  }, [route, navigate])
  const openTerminal = useCallback((session: Session) => {
    setTerminal(session)
    navigate({ session: session.id, view: 'terminal' })
  }, [navigate])

  const openChanges = useCallback((session: Session) => navigate({ session: session.id, view: 'changes' }), [navigate])

  const chatSession = listed ?? (terminal?.id === route.session ? terminal : undefined)
  let screen
  if (route.view === 'start') {
    screen = <StartSession start={start} onBack={back} />
  } else if (route.view === 'changes' && route.session && chatSession) {
    const id = route.session
    screen = route.file
      ? <FileDiffView key={`${id}:${route.file}`} session={chatSession} path={route.file} onBack={back} />
      : <ChangedFiles key={id} session={chatSession} onBack={back} onOpen={path => navigate({ session: id, view: 'changes', file: path })} />
  } else if (terminalTarget) {
    screen = <TerminalView key={`${terminalTarget.id}:${terminalTarget.runtime_binding}`} session={terminalTarget} onBack={back} />
  } else if (route.session && chatSession && (route.view === 'chat' || fallbackToChat)) {
    screen = <SessionChat key={route.session} initial={chatSession} onBack={back} onTerminal={openTerminal} onChanges={openChanges} />
  } else {
    // A deep link waits on the first list load under the list's own skeleton.
    screen = <SessionList sessions={route.session && !loaded ? [] : sessions} loaded={loaded} error={error} conditionalInput={conditionalInput} bridge={bridge} loadedBuild={loadedBuild} canStart={start.kinds.includes('claude')} onStart={() => navigate(startRoute)} onOpen={item => navigate({ session: item.id, view: 'chat' })} />
  }
  return <div className="app-shell" ref={shell}>{screen}</div>
}
