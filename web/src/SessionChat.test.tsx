import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { Session } from './api'
import { SessionChat } from './SessionChat'

const session: Session = { id: 'claude:a', agent: 'claude', pane_id: 'w1:p1', project: '/work/a', title: 'A', status: 'working', runtime_binding: 'b', chat: true, terminal: true, active: true, lifecycle: 'active' }
const render = (initial: Session) => renderToStaticMarkup(<SessionChat initial={initial} onBack={() => {}} onTerminal={() => {}} />)

beforeEach(() => {
  vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: () => {}, removeItem: () => {} })
  vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => {}, removeItem: () => {}, key: () => null, length: 0 })
})
afterEach(() => vi.unstubAllGlobals())

describe('draft restore', () => {
  it('restores the draft from the pending command, never from the device draft, while delivery is uncertain', () => {
    vi.stubGlobal('sessionStorage', { getItem: (key: string) => key === 'command:claude:a' ? JSON.stringify({ id: '00000000-0000-4000-8000-000000000001', binding: 'b', text: 'pending prompt' }) : null, setItem: () => {}, removeItem: () => {} })
    vi.stubGlobal('localStorage', { length: 1, key: () => 'draft:claude:a', getItem: (key: string) => key === 'draft:claude:a' ? JSON.stringify({ text: 'device draft', updated: 1 }) : null, setItem: () => {}, removeItem: () => {} })
    const html = render({ ...session, status: 'idle' })
    expect(html).toContain('pending prompt')
    expect(html).not.toContain('device draft')
  })
})

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
  it('opens the composer on a verified session that has no conversation yet', () => {
    const html = render({ ...session, status: 'idle' })
    expect(html).not.toMatch(/<textarea[^>]*disabled/)
    expect(html).not.toContain('대화를 찾지 못했습니다')
  })
  it('explains a session stopped at a startup screen instead of a missing conversation', () => {
    const html = render({ ...session, id: 'pane:w1:p1', status: 'needs_attention', runtime_binding: undefined, chat: false, terminal: false, lifecycle: 'unbound' })
    expect(html).toContain('시작 화면에서 기다리고 있습니다')
    expect(html).toContain('PC의 Herdr에서 시작 확인을 기다리고 있습니다.')
    expect(html).not.toContain('대화를 찾지 못했습니다')
    expect(html).not.toContain('원격 입력을 지원하지 않습니다')
    expect(html).toMatch(/<textarea[^>]*disabled/)
  })
  it('offers no Stop when the agent is not working', () => {
    expect(render({ ...session, status: 'idle' })).not.toContain('작업 중단')
    expect(render({ ...session, status: 'needs_attention' })).not.toContain('작업 중단')
  })
  it('names the project folder under the composer before the branch is read, and nothing without a project', () => {
    expect(render(session)).toMatch(/작업 폴더 <\/span><span[^>]*>a<\/span>/)
    expect(render({ ...session, project: '' })).not.toContain('작업 폴더')
  })
})
