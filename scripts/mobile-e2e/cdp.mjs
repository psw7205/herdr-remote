// Runs JavaScript against the Chrome tab reached through `android.sh up`'s
// DevTools forward (CDP_PORT, default 9222).
//
//   node scripts/mobile-e2e/cdp.mjs eval '<expression>'          print the value
//   node scripts/mobile-e2e/cdp.mjs run '<async body>'           helpers: send(method, params), ev(expr), sleep(ms), out(x)
//   node scripts/mobile-e2e/cdp.mjs center '<css selector>'      device px of the element center, for android.sh tap/hold
//
// TARGET=<url substring> picks the tab (default: the first page). `center`
// multiplies CSS px by the device pixel ratio and adds CONTENT_TOP, the device
// px where Chrome's content area starts (default 240: Pixel 4 status bar plus
// toolbar; read it from `uiautomator dump` as the bottom of control_container).
const [mode, arg] = process.argv.slice(2)
const port = process.env.CDP_PORT || '9222'
const timeout = Number(process.env.CDP_TIMEOUT || 60000)
const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
const pages = targets.filter(t => t.type === 'page')
const page = process.env.TARGET ? pages.find(t => t.url.includes(process.env.TARGET)) : pages[0]
if (!page) { console.error(JSON.stringify({ error: 'no page target', pages: pages.map(p => p.url) })); process.exit(1) }
const ws = new WebSocket(page.webSocketDebuggerUrl)
let id = 0
const pending = new Map()
const events = []
const send = (method, params = {}) => new Promise(resolve => { const i = ++id; pending.set(i, resolve); ws.send(JSON.stringify({ id: i, method, params })) })
ws.onmessage = e => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id) } else events.push(m) }
await new Promise(resolve => { ws.onopen = resolve })
const ev = async expression => {
  const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true, userGesture: true })
  if (r.result?.exceptionDetails) throw new Error(r.result.exceptionDetails.exception?.description ?? r.result.exceptionDetails.text)
  return r.result?.result?.value
}
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))
const out = x => console.log(typeof x === 'string' ? x : JSON.stringify(x))
const timer = setTimeout(() => { console.error('cdp: timeout'); process.exit(2) }, timeout)
try {
  if (mode === 'eval') out(await ev(arg))
  else if (mode === 'center') {
    const top = Number(process.env.CONTENT_TOP || 240)
    out(await ev(`(() => { const e = document.querySelector(${JSON.stringify(arg)}); if (!e) return 'none'
      const b = e.getBoundingClientRect(); const dpr = window.devicePixelRatio
      return Math.round((b.x + b.width / 2) * dpr) + ' ' + Math.round((b.y + b.height / 2) * dpr + ${top}) })()`))
  } else if (mode === 'run') {
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor
    await new AsyncFunction('send', 'ev', 'sleep', 'out', 'events', 'page', arg)(send, ev, sleep, out, events, page)
  } else { console.error('usage: cdp.mjs eval|run|center <arg>'); process.exit(2) }
} finally { clearTimeout(timer); ws.close() }
process.exit(0)
