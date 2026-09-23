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
