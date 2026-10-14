// Shared by the headless Chrome scripts: a production build of the fixture page
// served from a temporary directory, Chrome over the DevTools pipe, and a page
// with phone emulation.
import { spawn } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { build, preview } from 'vite'

export const webDir = resolve(fileURLToPath(new URL('..', import.meta.url)))
const chromeBin = process.env.CHROME_BIN || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const timeoutMs = 20_000

export class Cdp {
  #next = 0
  #pending = new Map()
  #waiters = new Set()
  #buffer = Buffer.alloc(0)

  constructor(input, output) {
    this.input = input
    output.on('data', chunk => this.#read(chunk))
    output.on('close', () => this.#fail(new Error('Chrome closed the DevTools pipe')))
  }

  #read(chunk) {
    this.#buffer = Buffer.concat([this.#buffer, chunk])
    for (let end = this.#buffer.indexOf(0); end >= 0; end = this.#buffer.indexOf(0)) {
      const message = JSON.parse(this.#buffer.subarray(0, end).toString('utf8'))
      this.#buffer = this.#buffer.subarray(end + 1)
      if (message.id !== undefined) {
        const pending = this.#pending.get(message.id)
        this.#pending.delete(message.id)
        if (message.error) pending?.reject(new Error(`${pending.method}: ${message.error.message}`))
        else pending?.resolve(message.result)
        continue
      }
      for (const waiter of this.#waiters) {
        if (waiter.method === message.method && waiter.sessionId === message.sessionId) {
          this.#waiters.delete(waiter)
          waiter.resolve(message.params)
        }
      }
    }
  }

  #fail(error) {
    for (const pending of this.#pending.values()) pending.reject(error)
    for (const waiter of this.#waiters) waiter.reject(error)
    this.#pending.clear()
    this.#waiters.clear()
  }

  send(method, params = {}, sessionId) {
    const id = ++this.#next
    return new Promise((resolve, reject) => {
      this.#pending.set(id, { method, resolve, reject })
      this.input.write(`${JSON.stringify({ id, method, params, sessionId })}\0`)
    })
  }

  once(method, sessionId) {
    let waiter
    const promise = new Promise((resolve, reject) => { waiter = { method, sessionId, resolve, reject } })
    this.#waiters.add(waiter)
    const timer = setTimeout(() => {
      this.#waiters.delete(waiter)
      waiter.reject(new Error(`timed out waiting for ${method}`))
    }, timeoutMs)
    return promise.finally(() => clearTimeout(timer))
  }
}

export function launchChrome(profile) {
  const chrome = spawn(chromeBin, [
    '--headless=new',
    '--remote-debugging-pipe',
    `--user-data-dir=${profile}`,
    '--no-first-run',
    '--no-default-browser-check',
    '--disable-background-networking',
    '--disable-component-update',
    '--disable-sync',
    '--disable-extensions',
    'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe', 'pipe', 'pipe'] })
  let stderr = ''
  chrome.stderr.on('data', chunk => { stderr = (stderr + chunk).slice(-4000) })
  const exited = new Promise(done => chrome.once('exit', (code, signal) => done({ code, signal })))
  const started = new Promise((done, fail) => {
    chrome.once('spawn', done)
    chrome.once('error', error => fail(new Error(`cannot start Chrome at ${chromeBin} (set CHROME_BIN): ${error.message}`)))
  })
  return { chrome, exited, started, stderr: () => stderr }
}

export async function stopChrome(cdp, launched) {
  if (launched.chrome.exitCode !== null || launched.chrome.signalCode !== null) return
  await Promise.race([cdp?.send('Browser.close').catch(() => {}), new Promise(done => setTimeout(done, 3000))])
  const exited = await Promise.race([launched.exited, new Promise(done => setTimeout(() => done(null), 3000))])
  if (exited === null) {
    launched.chrome.kill('SIGKILL')
    await launched.exited
  }
}

// A page in its own browser context, emulating a phone with touch. `url` is
// loaded before this returns; `close` drops the context.
export async function openPage(cdp, url, viewport) {
  const { browserContextId } = await cdp.send('Target.createBrowserContext')
  const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank', browserContextId })
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true })
  await cdp.send('Page.enable', {}, sessionId)
  await cdp.send('Network.enable', {}, sessionId)
  // A production build registers sw.js, whose navigation handler would answer
  // a later load of fixture.html with the cached index.html.
  await cdp.send('Network.setBypassServiceWorker', { bypass: true }, sessionId)
  await cdp.send('Emulation.setDeviceMetricsOverride', { ...viewport, deviceScaleFactor: 3, mobile: true }, sessionId)
  await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 }, sessionId)
  // Phones draw overlay scrollbars that take no layout width.
  await cdp.send('Emulation.setScrollbarsHidden', { hidden: true }, sessionId)
  const navigate = async target => {
    const loaded = cdp.once('Page.loadEventFired', sessionId)
    const navigation = await cdp.send('Page.navigate', { url: target }, sessionId)
    if (navigation.errorText) throw new Error(`navigation failed: ${navigation.errorText}`)
    await loaded
  }
  await navigate(url)
  return {
    sessionId,
    send: (method, params) => cdp.send(method, params, sessionId),
    navigate,
    async reload() {
      const loaded = cdp.once('Page.loadEventFired', sessionId)
      await cdp.send('Page.reload', {}, sessionId)
      await loaded
    },
    async evaluate(expression) {
      const result = await cdp.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true, userGesture: true }, sessionId)
      if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description ?? result.exceptionDetails.text)
      return result.result.value
    },
    close: () => cdp.send('Target.disposeBrowserContext', { browserContextId }).catch(() => {}),
  }
}

// Builds index.html and fixture.html into a temporary directory, serves them
// with vite preview, starts Chrome, runs `main({ cdp, product, url })` and
// cleans up. `url(path)` prefixes the preview origin.
export async function withFixturePage(main) {
  const temp = await mkdtemp(join(tmpdir(), 'herdr-headless-'))
  const outDir = join(temp, 'dist')
  const profile = join(temp, 'chrome')
  let server
  let launched
  let cdp
  const interrupt = signal => {
    launched?.chrome.kill('SIGKILL')
    void server?.close()
    void rm(temp, { recursive: true, force: true }).finally(() => process.exit(signal === 'SIGINT' ? 130 : 143))
  }
  process.once('SIGINT', interrupt)
  process.once('SIGTERM', interrupt)
  try {
    // Vite logs info to stdout; keep stdout for the JSON report.
    const shared = { root: webDir, logLevel: 'warn', build: { outDir } }
    // fixture.html is not a production entry, so this build adds it next to index.html.
    await build({ ...shared, build: { outDir, emptyOutDir: true, rolldownOptions: { input: { index: join(webDir, 'index.html'), fixture: join(webDir, 'fixture.html') } } } })
    // An empty proxy keeps a stray /api request from reaching a Bridge on 127.0.0.1:8787.
    server = await preview({ ...shared, preview: { host: '127.0.0.1', port: 0, strictPort: true, open: false, proxy: {} } })
    const { port } = server.httpServer.address()
    launched = launchChrome(profile)
    await launched.started
    cdp = new Cdp(launched.chrome.stdio[3], launched.chrome.stdio[4])
    const { product } = await cdp.send('Browser.getVersion')
    return await main({ cdp, product, url: path => `http://127.0.0.1:${port}${path}` })
  } catch (error) {
    if (launched) error.chromeStderr = launched.stderr()
    throw error
  } finally {
    if (launched) await stopChrome(cdp, launched)
    await server?.close()
    await rm(temp, { recursive: true, force: true })
    process.off('SIGINT', interrupt)
    process.off('SIGTERM', interrupt)
  }
}
