import type { Session } from './api'

// Hash routes keep the Bridge's static serving and the service worker's
// navigation handling unchanged: every view is the same document.
// 'start' has no session: it is the screen that asks Herdr for a new one.
// 'changes' lists the session project's changed files; with file it shows
// that file's diff, so Back goes diff → list → Chat.
export type View = 'chat' | 'terminal' | 'changes' | 'start'
export type Route = { session: string | null; view: View; file?: string }

export const listRoute: Route = { session: null, view: 'chat' }
export const startRoute: Route = { session: null, view: 'start' }

function decode(raw: string): string | null {
  try { return decodeURIComponent(raw) } catch { return null }
}

export function parseRoute(hash: string): Route {
  const params = new Map<string, string>()
  for (const part of hash.replace(/^#/, '').split('&')) {
    const index = part.indexOf('=')
    if (index > 0) params.set(part.slice(0, index), part.slice(index + 1))
  }
  const raw = params.get('session')
  if (!raw) return params.get('view') === 'start' ? startRoute : listRoute
  const session = decode(raw)
  if (session === null) return listRoute
  const view = params.get('view')
  if (view === 'terminal') return { session, view }
  if (view !== 'changes') return { session, view: 'chat' }
  const rawFile = params.get('file')
  const file = rawFile === undefined ? null : decode(rawFile)
  return file ? { session, view, file } : { session, view }
}

export function formatRoute(route: Route): string {
  if (!route.session) return route.view === 'start' ? '#view=start' : ''
  const session = `#session=${encodeURIComponent(route.session)}`
  if (route.view === 'terminal') return `${session}&view=terminal`
  if (route.view !== 'changes') return session
  return route.file ? `${session}&view=changes&file=${encodeURIComponent(route.file)}` : `${session}&view=changes`
}

export function parentRoute(route: Route): Route {
  if (!route.session) return listRoute
  if (route.view === 'changes' && route.file) return { session: route.session, view: 'changes' }
  return route.view === 'terminal' || route.view === 'changes' ? { session: route.session, view: 'chat' } : listRoute
}

// History only lets the current entry be rewritten, so older entries still name
// a pane: item after it was superseded. aliases maps it to its successor.
export function followAlias(route: Route, aliases: ReadonlyMap<string, string>): Route {
  let session = route.session
  for (let hops = 0; session && aliases.has(session) && hops < 8; hops += 1) session = aliases.get(session) ?? session
  return session === route.session ? route : { ...route, session }
}

// The Terminal keeps the binding it opened with. A later binding under the same
// id is not followed silently: for a pane: item it can be another process.
export function resolveTerminal(route: Route, captured: Session | null, listed: Session | undefined): Session | null {
  if (route.view !== 'terminal' || !route.session) return null
  if (captured?.id === route.session) return captured
  return listed?.terminal ? listed : null
}

// A pane: item re-keyed to its successor keeps the view it was on. The
// Terminal needs the successor's own Terminal; Changes needs no binding.
export function successorRoute(current: Route, successor: Pick<Session, 'id' | 'terminal'>): Route {
  if (current.view === 'terminal') return { session: successor.id, view: successor.terminal ? 'terminal' : 'chat' }
  if (current.view === 'changes') return { ...current, session: successor.id }
  return { session: successor.id, view: 'chat' }
}

// depth counts the history entries this app pushed above the current one. A
// deep link has none, so going back replaces it with the parent view instead
// of leaving the app.
export type BackAction = { kind: 'history' } | { kind: 'replace'; route: Route }
export function backAction(route: Route, depth: number): BackAction {
  return depth > 0 ? { kind: 'history' } : { kind: 'replace', route: parentRoute(route) }
}
