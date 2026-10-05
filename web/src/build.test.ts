import { describe, expect, it } from 'vitest'
import type { BridgeBuild } from './api'
import { bridgeBuildLabel, clientBuildStale, loadedClientBuild } from './build'

const head = (content: string | null) => ({ querySelector: () => content === null ? null : { getAttribute: () => content } })
const bridge: BridgeBuild = { revision: '65f5c6bd7eceb1cd601405dec32f04078918f161', modified: false, clientBuild: '0123456789abcdef' }

describe('build identity', () => {
  it('reads the stamped client build from the document head', () => {
    expect(loadedClientBuild(head('0123456789abcdef'))).toBe('0123456789abcdef')
  })
  it('treats the dev placeholder and a missing meta as unknown', () => {
    expect(loadedClientBuild(head('__BUILD_ID__'))).toBeNull()
    expect(loadedClientBuild(head(null))).toBeNull()
    expect(loadedClientBuild(null)).toBeNull()
  })
  it('labels the Bridge by short revision, with dirty and dev states', () => {
    expect(bridgeBuildLabel(bridge)).toBe('65f5c6b')
    expect(bridgeBuildLabel({ ...bridge, modified: true })).toBe('65f5c6b-dirty')
    expect(bridgeBuildLabel({ ...bridge, revision: '' })).toBe('dev')
    expect(bridgeBuildLabel(null)).toBeNull()
  })
  it('flags a stale client only when both ids are known and differ', () => {
    expect(clientBuildStale(bridge, 'fedcba9876543210')).toBe(true)
    expect(clientBuildStale(bridge, '0123456789abcdef')).toBe(false)
    expect(clientBuildStale({ ...bridge, clientBuild: '' }, 'fedcba9876543210')).toBe(false)
    expect(clientBuildStale(bridge, null)).toBe(false)
    expect(clientBuildStale(null, 'fedcba9876543210')).toBe(false)
  })
})
