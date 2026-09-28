import { describe, expect, it } from 'vitest'
import { buildIdPlaceholder, computeBuildId, injectBuildId } from './swBuildId'

describe('service worker build id', () => {
  it('replaces every placeholder', () => {
    expect(injectBuildId(`const a = 'x-${buildIdPlaceholder}'; const b = '${buildIdPlaceholder}'`, 'abc')).toBe("const a = 'x-abc'; const b = 'abc'")
  })

  it('fails when the placeholder is missing', () => {
    expect(() => injectBuildId("const shell = 'herdr-chat-shell-v3'", 'abc')).toThrow(buildIdPlaceholder)
  })

  it('is stable across file order and changes with content', async () => {
    const files = [{ name: 'index.html', content: '<html>' }, { name: 'assets/a.js', content: new Uint8Array([1, 2]) }]
    const id = await computeBuildId(files)
    expect(id).toMatch(/^[0-9a-f]{16}$/)
    expect(await computeBuildId([...files].reverse())).toBe(id)
    expect(await computeBuildId([files[0], { name: 'assets/a.js', content: new Uint8Array([1, 3]) }])).not.toBe(id)
  })

  it('separates file names from contents', async () => {
    expect(await computeBuildId([{ name: 'a', content: 'bc' }])).not.toBe(await computeBuildId([{ name: 'ab', content: 'c' }]))
  })
})
