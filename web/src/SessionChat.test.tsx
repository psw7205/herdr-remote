import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { Session } from './api'
import { SessionChat } from './SessionChat'

const session: Session = { id: 'claude:a', agent: 'claude', pane_id: 'w1:p1', project: '/work/a', title: 'A', status: 'working', runtime_binding: 'b', chat: true, terminal: true, active: true, lifecycle: 'active' }
const render = (initial: Session) => renderToStaticMarkup(<SessionChat initial={initial} onBack={() => {}} onTerminal={() => {}} />)

beforeEach(() => { vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: () => {}, removeItem: () => {} }) })
afterEach(() => vi.unstubAllGlobals())

describe('composer controls', () => {
  it('offers Stop as its own action while the agent works, never in the Send slot', () => {
    const html = render(session)
    expect(html).toContain('>작업 중단<')
    expect(html).not.toContain('aria-label="중단"')
    expect(html).toMatch(/<button type="submit"[^>]*aria-label="보내기"[^>]*disabled/)
  })
  it('shows a Codex session read-only with the reason', () => {
    const html = render({ ...session, id: 'codex:a', agent: 'codex', status: 'working', runtime_binding: undefined, terminal: false, lifecycle: 'unbound' })
    expect(html).toContain('Codex 입력은 아직 지원하지 않습니다.')
    expect(html).toMatch(/<textarea[^>]*disabled/)
    expect(html).toMatch(/<button type="submit"[^>]*disabled/)
    expect(html).not.toContain('작업 중단')
    expect(html).not.toContain('Terminal 열기')
    expect(html).not.toContain('대화를 찾지 못했습니다')
  })
  it('offers no Stop when the agent is not working', () => {
    expect(render({ ...session, status: 'idle' })).not.toContain('작업 중단')
    expect(render({ ...session, status: 'needs_attention' })).not.toContain('작업 중단')
  })
})
