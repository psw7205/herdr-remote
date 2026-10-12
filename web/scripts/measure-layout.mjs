// jsdom has no layout, so horizontal scrolling of the `wide-table` fixture is
// measured here in headless Chrome at phone widths.
import { spawn } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { build, preview } from 'vite'

const webDir = resolve(fileURLToPath(new URL('..', import.meta.url)))
const chromeBin = process.env.CHROME_BIN || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const viewports = [{ width: 360, height: 780 }, { width: 390, height: 844 }, { width: 412, height: 915 }]
const scenario = 'wide-table'
const session = 'claude:fixture-working'
const scrollProbe = 200
const timeoutMs = 20_000

class Cdp {
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

// Runs in the page. Selectors avoid CSS module class names, which the build hashes.
async function measurePage({ width, probe, timeout }) {
  const deadline = performance.now() + timeout
  const frame = () => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done)))
  const columns = container => container.querySelector('tr')?.children.length ?? 0
  let tables = []
  let codes = []
  while (tables.length === 0 || codes.length === 0) {
    if (performance.now() > deadline) throw new Error('the fixture table and code block did not render')
    await new Promise(done => setTimeout(done, 100))
    tables = [...document.querySelectorAll('[data-scroll="x"]')].filter(container => container.querySelector('table'))
    codes = [...document.querySelectorAll('figure pre')]
  }
  const table = tables.reduce((widest, item) => (columns(item) > columns(widest) ? item : widest))
  const code = codes.reduce((widest, item) => (item.scrollWidth > widest.scrollWidth ? item : widest))
  const chat = document.querySelector('main[aria-label="대화"]')
  const page = () => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    innerWidth: window.innerWidth,
    scrollX: window.scrollX,
    visualScale: window.visualViewport?.scale ?? 1,
    chatScrollWidth: chat?.scrollWidth ?? null,
    chatClientWidth: chat?.clientWidth ?? null,
  })
  const pageFits = state => state.scrollWidth <= width && state.scrollWidth <= state.innerWidth && state.scrollX === 0
    && state.chatScrollWidth !== null && state.chatScrollWidth <= state.chatClientWidth
  const probeScroll = async element => {
    element.scrollIntoView({ block: 'center', inline: 'nearest' })
    await frame()
    const clientWidth = element.clientWidth
    const scrollWidth = element.scrollWidth
    element.scrollLeft = probe
    await frame()
    const scrollLeft = element.scrollLeft
    const during = page()
    element.scrollLeft = 0
    await frame()
    return { clientWidth, scrollWidth, overflows: scrollWidth > clientWidth, scrollLeft, scrolls: Math.round(scrollLeft) === probe, pageFitsWhileScrolled: pageFits(during), pageWhileScrolled: during }
  }

  const tableScroll = await probeScroll(table)
  const element = table.querySelector('table')
  const cells = [...element.querySelectorAll('tbody td')].map(cell => cell.getBoundingClientRect().width)
  const rows = [...element.querySelectorAll('tbody tr')].map(row => row.getBoundingClientRect().height)
  const renderedWidth = element.offsetWidth
  element.style.width = 'max-content'
  const maxContentWidth = element.offsetWidth
  element.style.width = ''
  await frame()
  const codeScroll = await probeScroll(code)
  const final = page()
  const result = {
    table: { columns: columns(table), rows: rows.length, renderedWidth, maxContentWidth, narrowestCell: Math.round(Math.min(...cells)), tallestRow: Math.round(Math.max(...rows)), ...tableScroll },
    code: codeScroll,
    page: { ...final, fits: pageFits(final) },
  }
  result.pass = result.table.columns >= 12 && result.table.overflows && result.table.scrolls && result.table.pageFitsWhileScrolled
    && result.code.overflows && result.code.scrolls && result.code.pageFitsWhileScrolled && result.page.fits
  return result
}

async function measure(cdp, url, viewport) {
  const { browserContextId } = await cdp.send('Target.createBrowserContext')
  try {
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
    const loaded = cdp.once('Page.loadEventFired', sessionId)
    const navigation = await cdp.send('Page.navigate', { url }, sessionId)
    if (navigation.errorText) throw new Error(`navigation failed: ${navigation.errorText}`)
    await loaded
    const args = JSON.stringify({ width: viewport.width, probe: scrollProbe, timeout: timeoutMs })
    const evaluation = await cdp.send('Runtime.evaluate', { expression: `(${measurePage})(${args})`, awaitPromise: true, returnByValue: true }, sessionId)
    if (evaluation.exceptionDetails) throw new Error(evaluation.exceptionDetails.exception?.description ?? evaluation.exceptionDetails.text)
    return { ...viewport, ...evaluation.result.value }
  } finally {
    await cdp.send('Target.disposeBrowserContext', { browserContextId }).catch(() => {})
  }
}

function launchChrome(profile) {
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

async function stopChrome(cdp, launched) {
  if (launched.chrome.exitCode !== null || launched.chrome.signalCode !== null) return
  await Promise.race([cdp?.send('Browser.close').catch(() => {}), new Promise(done => setTimeout(done, 3000))])
  const exited = await Promise.race([launched.exited, new Promise(done => setTimeout(() => done(null), 3000))])
  if (exited === null) {
    launched.chrome.kill('SIGKILL')
    await launched.exited
  }
}

async function main() {
  const temp = await mkdtemp(join(tmpdir(), 'herdr-measure-'))
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
    const url = `http://127.0.0.1:${port}/fixture.html?scenario=${scenario}#session=${encodeURIComponent(session)}`

    launched = launchChrome(profile)
    await launched.started
    cdp = new Cdp(launched.chrome.stdio[3], launched.chrome.stdio[4])
    const { product } = await cdp.send('Browser.getVersion')
    const results = []
    for (const viewport of viewports) results.push(await measure(cdp, url, viewport))
    const report = { scenario, chrome: product, scrollProbe, results, pass: results.every(result => result.pass) }
    process.stdout.write(`${JSON.stringify(report, null, 2)}\n`)
    if (!report.pass) process.exitCode = 1
  } catch (error) {
    process.stderr.write(`measure-layout: ${error.stack ?? error}\n`)
    if (launched) process.stderr.write(`Chrome stderr (tail):\n${launched.stderr()}\n`)
    process.exitCode = 1
  } finally {
    if (launched) await stopChrome(cdp, launched)
    await server?.close()
    await rm(temp, { recursive: true, force: true })
    process.off('SIGINT', interrupt)
    process.off('SIGTERM', interrupt)
  }
}

await main()
