import type { BridgeBuild } from './api'

type Head = { querySelector(selector: string): { getAttribute(name: string): string | null } | null }
const buildIdPattern = /^[0-9a-f]{16}$/

// The Vite build stamps the bundle hash into index.html (web/plugins/buildId.ts).
// The dev server serves the placeholder, so a dev page is unknown, never stale.
export function loadedClientBuild(doc: Head | null): string | null {
  const content = doc?.querySelector('meta[name="herdr-build"]')?.getAttribute('content') ?? null
  return content && buildIdPattern.test(content) ? content : null
}

export function bridgeBuildLabel(bridge: BridgeBuild | null): string | null {
  if (!bridge) return null
  if (!bridge.revision) return 'dev'
  return `${bridge.revision.slice(0, 7)}${bridge.modified ? '-dirty' : ''}`
}

export function clientBuildStale(bridge: BridgeBuild | null, loaded: string | null): boolean {
  return Boolean(bridge?.clientBuild && loaded && bridge.clientBuild !== loaded)
}
