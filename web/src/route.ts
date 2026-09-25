import type { Session } from './api'

// Hash routes keep the Bridge's static serving and the service worker's
// navigation handling unchanged: every view is the same document.
export type View = 'chat' | 'terminal'
export type Route = { session: string | null; view: View }

export const listRoute: Route = { session: null, view: 'chat' }

export function parseRoute(hash: string): Route {
  const params = new Map<string, string>()
  for (const part of hash.replace(/^#/, '').split('&')) {
    const index = part.indexOf('=')
    if (index > 0) params.set(part.slice(0, index), part.slice(index + 1))
  }
  const raw = params.get('session')
  if (!raw) return listRoute
  let session: string
  try { session = decodeURIComponent(raw) } catch { return listRoute }
  return { session, view: params.get('view') === 'terminal' ? 'terminal' : 'chat' }
}

export function formatRoute(route: Route): string {
  if (!route.session) return ''
  const session = `#session=${encodeURIComponent(route.session)}`
  return route.view === 'terminal' ? `${session}&view=terminal` : session
}

export function parentRoute(route: Route): Route {
  return route.view === 'terminal' && route.session ? { session: route.session, view: 'chat' } : listRoute
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

// depth counts the history entries this app pushed above the current one. A
// deep link has none, so going back replaces it with the parent view instead
// of leaving the app.
export type BackAction = { kind: 'history' } | { kind: 'replace'; route: Route }
export function backAction(route: Route, depth: number): BackAction {
  return depth > 0 ? { kind: 'history' } : { kind: 'replace', route: parentRoute(route) }
}
