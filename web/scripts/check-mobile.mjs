// Phone input and touch checks on the fixture page in headless Chrome.
// Terminal: drives xterm's hidden textarea the way a phone IME does (keyCode
// 229 keydowns, in-place edits, compositions) and checks what reaches
// POST /commands, plus that input does not scroll the Terminal while a finger
// is on it. Chat: drags the conversation up while its content keeps growing
// and checks that the reader is not pulled back to the bottom.
import { openPage, withFixturePage } from './headless.mjs'

const DEL = '\x7f'
const route = '/fixture.html?scenario=default#session=claude%3Afixture-working&view=terminal'
const chatRoute = '/fixture.html?scenario=long#session=claude%3Afixture-working'
const sleep = ms => new Promise(done => setTimeout(done, ms))

const textareaReady = `!!document.querySelector('.xterm textarea') && (document.querySelector('.xterm-rows')?.textContent ?? '').trim().length > 0`
const recordPosts = `(() => {
  window.__posts = []
  const real = globalThis.fetch
  globalThis.fetch = async (input, init) => {
    const url = String(input instanceof Request ? input.url : input)
    if (init?.method === 'POST' && url.includes('/commands')) window.__posts.push(JSON.parse(init.body).payload.text)
    return real(input, init)
  }
  return true
})()`
const textarea = `document.querySelector('.xterm textarea')`
const state = `({ value: ${textarea}.value, sent: window.__posts.splice(0) })`
// The browser fires input after the IME changed the field; a script assignment does not.
const imeEdit = (value, inputType = 'insertText') => `(() => { const ta = ${textarea}; ta.value = ${JSON.stringify(value)}; ta.dispatchEvent(new InputEvent('input', { inputType: ${JSON.stringify(inputType)}, bubbles: true })); return true })()`
const composition = (type, data) => `(() => { ${textarea}.dispatchEvent(new CompositionEvent(${JSON.stringify(type)}, { data: ${JSON.stringify(data)}, bubbles: true })); return true })()`

async function waitFor(page, expression, timeout = 20_000) {
  const deadline = Date.now() + timeout
  for (;;) {
    if (await page.evaluate(expression)) return
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${expression}`)
    await sleep(100)
  }
}

async function imeKey(page, type) {
  await page.send('Input.dispatchKeyEvent', { type, key: 'Unidentified', windowsVirtualKeyCode: 229, nativeVirtualKeyCode: 229 })
}

// One IME keystroke: keydown 229, the field changes, keyup 229.
async function imeStroke(page, value, keydowns = 1) {
  for (let i = 0; i < keydowns; i++) await imeKey(page, 'keyDown')
  await page.evaluate(imeEdit(value))
  await imeKey(page, 'keyUp')
  await sleep(150)
  return page.evaluate(state)
}

// A composition: each step is the whole field value while the IME shows that
// composing text; the last one is what it commits.
async function compose(page, steps) {
  await imeKey(page, 'keyDown')
  await page.evaluate(composition('compositionstart', ''))
  for (const [value, data] of steps) {
    await page.evaluate(composition('compositionupdate', data))
    await page.evaluate(imeEdit(value, 'insertCompositionText'))
  }
  const [committed, data] = steps.at(-1)
  await page.evaluate(composition('compositionend', data))
  await page.evaluate(imeEdit(committed, 'insertFromComposition'))
  await imeKey(page, 'keyUp')
  await sleep(150)
  return page.evaluate(state)
}

async function inputChecks(page) {
  await page.evaluate(recordPosts)
  await page.evaluate(`${textarea}.focus(); true`)
  const checks = []
  const check = (name, observed, pass) => checks.push({ name, ...observed, pass })

  await page.send('Input.insertText', { text: 'ab' })
  await sleep(150)
  let got = await page.evaluate(state)
  check('IME commit sends the text once', got, got.value === 'ab' && got.sent.join('|') === 'ab')

  await page.evaluate(`document.querySelector('button[aria-label="Enter"]').click(); true`)
  await sleep(150)
  got = await page.evaluate(state)
  check('key bar Enter sends CR and empties the textarea', got, got.value === '' && got.sent.join('|') === '\r')

  got = await imeStroke(page, 'ab')
  check('typing after Enter starts from an empty field', got, got.sent.join('|') === 'ab')
  got = await imeStroke(page, 'ac')
  check('a same-length edit after keyCode 229 sends only the edit', got, got.sent.join('|') === `${DEL}c`)
  got = await imeStroke(page, 'acgo', 2)
  check('two keyCode 229 keydowns before one change send it once', got, got.sent.join('|') === 'go')

  got = await compose(page, [['acgoㅌ', 'ㅌ'], ['acgo토', '토'], ['acgo통', '통']])
  check('a composed syllable is sent once', got, got.value === 'acgo통' && got.sent.join('|') === '통')
  got = await compose(page, [['acgo톹', '톹'], ['acgo통토', '통토'], ['acgo통통', '통통']])
  check('recomposition over the last syllable sends the new syllable once', got, got.value === 'acgo통통' && got.sent.join('|') === '통')

  // Real keys (a hardware keyboard, or a soft keyboard reporting real codes).
  // xterm sends a lower-case letter from keydown and cancels it, so the field
  // does not change; an upper-case letter goes out from keypress and lands in
  // the field too, which must not be sent again.
  const realKey = async (key, modifiers = 0) => {
    await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key, code: 'KeyA', text: key, windowsVirtualKeyCode: 65, nativeVirtualKeyCode: 65, modifiers })
    await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key, code: 'KeyA', windowsVirtualKeyCode: 65, nativeVirtualKeyCode: 65, modifiers })
    await sleep(150)
    return page.evaluate(state)
  }
  got = await realKey('a')
  check('a real key is sent once by xterm', got, got.value === 'acgo통통' && got.sent.join('|') === 'a')
  got = await realKey('A', 8)
  check('a real upper-case key that lands in the textarea is sent once', got, got.value === 'acgo통통A' && got.sent.join('|') === 'A')
  got = await imeStroke(page, 'acgo통통Ab')
  check('IME typing after real keys continues from the synced field', got, got.sent.join('|') === 'b')
  return checks
}

async function scrollChecks(page) {
  await page.evaluate(`localStorage.setItem('terminal-font', '32'); true`)
  await page.reload()
  await waitFor(page, textareaReady)
  await sleep(800)
  await page.evaluate(recordPosts)
  const shell = `document.querySelector('.xterm').parentElement`
  const metrics = await page.evaluate(`(() => { const s = ${shell}; const r = s.getBoundingClientRect(); return { scrollHeight: s.scrollHeight, clientHeight: s.clientHeight, x: Math.round(r.left + r.width / 2), y: Math.round(r.top + r.height / 2) } })()`)
  const checks = []
  if (metrics.scrollHeight <= metrics.clientHeight) {
    checks.push({ name: 'Terminal shell overflows at a large font', ...metrics, pass: false })
    return checks
  }
  // The key bar sends without touching the textarea, so only the app's own
  // reveal can move the view; typing would also make the browser show the caret.
  const esc = `[...document.querySelectorAll('button')].find(button => button.textContent === 'Esc').click()`
  await page.evaluate(`${textarea}.focus({ preventScroll: true }); ${shell}.scrollTop = 0; true`)
  await sleep(100)
  const before = await page.evaluate(`${shell}.scrollTop`)
  await page.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: metrics.x, y: metrics.y }] })
  await page.evaluate(`${esc}; true`)
  await sleep(300)
  const during = await page.evaluate(`({ scrollTop: ${shell}.scrollTop, sent: window.__posts.splice(0) })`)
  checks.push({ name: 'key bar input while a finger is down does not scroll the Terminal', before, ...during, pass: before === 0 && during.scrollTop === 0 && during.sent.join('|') === '\x1b' })
  await page.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  await page.evaluate(`${esc}; true`)
  await sleep(300)
  const after = await page.evaluate(`({ scrollTop: ${shell}.scrollTop, max: ${shell}.scrollHeight - ${shell}.clientHeight, sent: window.__posts.splice(0) })`)
  checks.push({ name: 'key bar input after the finger lifts brings the cursor line into view', ...after, pass: after.scrollTop > 0 && after.sent.join('|') === '\x1b' })
  return checks
}

const chat = `document.querySelector('main[aria-label="대화"]')`
const chatState = `(() => { const el = ${chat}; return { scrollTop: Math.round(el.scrollTop), max: el.scrollHeight - el.clientHeight, snaps: window.__snaps } })()`
// A little more content under the reader, like a Markdown reflow or a new tool row.
const grow = height => `(() => { const row = document.createElement('div'); row.style.height = '${height}px'; ${chat}.firstElementChild.append(row); return true })()`

async function chatChecks(page) {
  await waitFor(page, `document.querySelectorAll('main[aria-label="대화"] article').length > 300`)
  await sleep(500)
  // Every scrollTop assignment is a snap; the reader's own scrolling never assigns.
  await page.evaluate(`(() => {
    const el = ${chat}
    const desc = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollTop')
    window.__snaps = 0
    Object.defineProperty(el, 'scrollTop', { configurable: true, get() { return desc.get.call(this) }, set(value) { window.__snaps++; desc.set.call(this, value) } })
    return true
  })()`)
  const box = await page.evaluate(`(() => { const r = ${chat}.getBoundingClientRect(); return { x: Math.round(r.left + r.width / 2), y: Math.round(r.top + r.height * 0.35) } })()`)
  const toBottom = async () => { await page.evaluate(`${chat}.scrollTop = ${chat}.scrollHeight; true`); await sleep(400); await page.evaluate(`window.__snaps = 0; true`) }
  // The finger moves down the screen, so the content scrolls up, and the content grows under it on every move.
  const drag = async (distance, steps) => {
    await page.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: box.x, y: box.y }] })
    for (let i = 1; i <= steps; i++) {
      await page.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: box.x, y: box.y + distance * i / steps }] })
      await page.evaluate(grow(6))
      await sleep(40)
    }
    await page.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  }
  const checks = []
  await toBottom()
  const start = await page.evaluate(chatState)
  await drag(300, 12)
  const released = await page.evaluate(chatState)
  checks.push({ name: 'a drag up while the content grows is not pulled back', start: start.scrollTop, ...released, pass: released.snaps === 0 && released.scrollTop <= start.max - 200 })
  await sleep(400)
  const away = await page.evaluate(chatState)
  const jump = await page.evaluate(`!!document.querySelector('button svg.lucide-arrow-down')`)
  checks.push({ name: 'letting go above the bottom band keeps the place and shows the jump button', ...away, jump, pass: away.snaps === 0 && away.scrollTop === released.scrollTop && jump })

  await toBottom()
  await drag(40, 4)
  const peek = await page.evaluate(chatState)
  await sleep(400)
  const settled = await page.evaluate(chatState)
  checks.push({ name: 'a short peek inside the bottom band follows again once the finger lifts', peek: peek.scrollTop, snapsDuring: peek.snaps, ...settled, pass: peek.snaps === 0 && settled.scrollTop === settled.max })

  await page.evaluate(`window.__snaps = 0; true`)
  await page.evaluate(grow(120))
  await sleep(300)
  const idle = await page.evaluate(chatState)
  checks.push({ name: 'growth with no finger down keeps the reader at the bottom', ...idle, pass: idle.snaps >= 1 && idle.scrollTop === idle.max })
  return checks
}

try {
  const report = await withFixturePage(async ({ cdp, product, url }) => {
    const checks = []
    const run = async (path, viewport, probe) => {
      const page = await openPage(cdp, url(path), viewport)
      try { checks.push(...await probe(page)) } finally { await page.close() }
    }
    await run(route, { width: 390, height: 844 }, async page => { await waitFor(page, textareaReady); return inputChecks(page) })
    await run(route, { width: 390, height: 420 }, async page => { await waitFor(page, textareaReady); return scrollChecks(page) })
    await run(chatRoute, { width: 390, height: 844 }, chatChecks)
    return { chrome: product, checks, pass: checks.every(item => item.pass) }
  })
  // Control characters in the sent text are shown by name.
  const visible = (key, value) => typeof value === 'string' ? value.replace(/\x7f/g, '<DEL>').replace(/\r/g, '<CR>').replace(/\x1b/g, '<ESC>') : value
  process.stdout.write(`${JSON.stringify(report, visible, 2)}\n`)
  if (!report.pass) process.exitCode = 1
} catch (error) {
  process.stderr.write(`check-mobile-input: ${error.stack ?? error}\n`)
  if (error.chromeStderr) process.stderr.write(`Chrome stderr (tail):\n${error.chromeStderr}\n`)
  process.exitCode = 1
}
