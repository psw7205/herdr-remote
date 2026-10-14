// jsdom has no layout, so horizontal scrolling of the `wide-table` fixture is
// measured here in headless Chrome at phone widths.
import { openPage, withFixturePage } from './headless.mjs'

const viewports = [{ width: 360, height: 780 }, { width: 390, height: 844 }, { width: 412, height: 915 }]
const scenario = 'wide-table'
const session = 'claude:fixture-working'
const scrollProbe = 200
const timeoutMs = 20_000

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
  const page = await openPage(cdp, url, viewport)
  try {
    const args = JSON.stringify({ width: viewport.width, probe: scrollProbe, timeout: timeoutMs })
    return { ...viewport, ...await page.evaluate(`(${measurePage})(${args})`) }
  } finally {
    await page.close()
  }
}

try {
  const report = await withFixturePage(async ({ cdp, product, url }) => {
    const target = url(`/fixture.html?scenario=${scenario}#session=${encodeURIComponent(session)}`)
    const results = []
    for (const viewport of viewports) results.push(await measure(cdp, target, viewport))
    return { scenario, chrome: product, scrollProbe, results, pass: results.every(result => result.pass) }
  })
  process.stdout.write(`${JSON.stringify(report, null, 2)}\n`)
  if (!report.pass) process.exitCode = 1
} catch (error) {
  process.stderr.write(`measure-layout: ${error.stack ?? error}\n`)
  if (error.chromeStderr) process.stderr.write(`Chrome stderr (tail):\n${error.chromeStderr}\n`)
  process.exitCode = 1
}
