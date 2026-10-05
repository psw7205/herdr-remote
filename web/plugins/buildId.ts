import type { Plugin } from 'vite'

// `public/sw.js` names its cache with this placeholder and `index.html` carries
// it in a `herdr-build` meta. The build replaces both with a hash of the emitted
// bundle: every deploy that changes the client ships a byte-different service
// worker whose `activate` drops older caches, and the page and the Bridge
// (internal/httpapi reads the same meta) can tell which client build is served.
export const buildIdPlaceholder = '__BUILD_ID__'

export type BuildFile = { name: string, content: string | Uint8Array }
export type StampFS = {
  readFile(path: string, options: { encoding: 'utf8' }): Promise<string>
  writeFile(path: string, data: string): Promise<void>
}

export function injectBuildId(source: string, id: string, name = 'service worker'): string {
  if (!source.includes(buildIdPlaceholder)) throw new Error(`${name} has no ${buildIdPlaceholder} placeholder`)
  return source.replaceAll(buildIdPlaceholder, id)
}

export async function computeBuildId(files: readonly BuildFile[]): Promise<string> {
  const encoder = new TextEncoder()
  const parts: Uint8Array[] = []
  const sorted = [...files].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
  for (const file of sorted) {
    const content = typeof file.content === 'string' ? encoder.encode(file.content) : file.content
    parts.push(encoder.encode(`${file.name}\0${content.byteLength}\0`), content)
  }
  const joined = new Uint8Array(parts.reduce((size, part) => size + part.byteLength, 0))
  let offset = 0
  for (const part of parts) {
    joined.set(part, offset)
    offset += part.byteLength
  }
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', joined))
  return Array.from(digest.subarray(0, 8), byte => byte.toString(16).padStart(2, '0')).join('')
}

export async function stampBuildId(fs: StampFS, dir: string, fileNames: readonly string[], id: string): Promise<void> {
  for (const name of fileNames) {
    const path = `${dir}/${name}`
    const source = await fs.readFile(path, { encoding: 'utf8' })
    await fs.writeFile(path, injectBuildId(source, id, name))
  }
}

export function buildIdStamp(fileNames: readonly string[] = ['sw.js', 'index.html']): Plugin {
  return {
    name: 'herdr:build-id',
    apply: 'build',
    enforce: 'post',
    async writeBundle(options, bundle) {
      if (!options.dir) this.error('build id stamping needs build.outDir')
      const id = await computeBuildId(Object.values(bundle).map(output => ({
        name: output.fileName,
        content: output.type === 'chunk' ? output.code : output.source,
      })))
      await stampBuildId(this.fs, options.dir, fileNames, id)
    },
  }
}
