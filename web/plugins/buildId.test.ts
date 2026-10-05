import { describe, expect, it } from 'vitest'
import { buildIdPlaceholder, computeBuildId, injectBuildId, stampBuildId } from './buildId'

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

describe('build id stamping', () => {
  const memory = (files: Record<string, string>) => ({
    files,
    fs: {
      readFile: async (path: string) => { const content = files[path]; if (content === undefined) throw new Error(`missing ${path}`); return content },
      writeFile: async (path: string, data: string) => { files[path] = data },
    },
  })
  it('writes the same id into every stamped file', async () => {
    const { files, fs } = memory({ 'dist/sw.js': `const shell = 'shell-${buildIdPlaceholder}'`, 'dist/index.html': `<meta name="herdr-build" content="${buildIdPlaceholder}" />` })
    await stampBuildId(fs, 'dist', ['sw.js', 'index.html'], '0123456789abcdef')
    expect(files['dist/sw.js']).toBe("const shell = 'shell-0123456789abcdef'")
    expect(files['dist/index.html']).toBe('<meta name="herdr-build" content="0123456789abcdef" />')
  })
  it('names the file whose placeholder is missing', async () => {
    const { fs } = memory({ 'dist/sw.js': `'${buildIdPlaceholder}'`, 'dist/index.html': '<meta name="herdr-build" content="v1" />' })
    await expect(stampBuildId(fs, 'dist', ['sw.js', 'index.html'], 'abc')).rejects.toThrow('index.html')
  })
})
