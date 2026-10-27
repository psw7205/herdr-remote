import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { Composer } from './Composer'

const render = (place?: { folder: string; branch?: string } | null) => renderToStaticMarkup(
  <Composer draft="" onDraft={() => {}} canType canSend sending={false} onSend={() => {}} status={null} place={place} />,
)

describe('working place', () => {
  it('shows the folder and branch under the field', () => {
    const html = render({ folder: 'sample-api', branch: 'feature/long-branch-name' })
    expect(html).toMatch(/<\/textarea>.*sample-api.*feature\/long-branch-name/)
    expect(html).toContain('title="feature/long-branch-name"')
  })
  it('shows the folder alone outside a repository, on a detached HEAD or after an error', () => {
    const html = render({ folder: 'sample-api' })
    expect(html).toContain('sample-api')
    expect(html).not.toContain('branch')
  })
  it('adds nothing without a project', () => {
    expect(render(null)).not.toContain('작업 폴더')
    expect(render()).not.toContain('작업 폴더')
  })
})
