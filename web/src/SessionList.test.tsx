import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { Session } from './api'
import { SessionList } from './SessionList'

const session: Session = { id: 'claude:a', agent: 'claude', pane_id: 'w1:p1', project: '/work/sample-api', title: '결제 API 보강', status: 'needs_attention', runtime_binding: 'b', chat: true, terminal: true, active: true, lifecycle: 'active', last_activity: new Date().toISOString(), last_message: { role: 'user', text: '**실패** case를 추가해 줘' } }
const render = (props: Partial<Parameters<typeof SessionList>[0]>) => renderToStaticMarkup(<SessionList sessions={[]} loaded error="" conditionalInput="supported" onOpen={() => {}} {...props} />)

describe('session list', () => {
  it('shows a row with status, project and a plain preview under its group', () => {
    const html = render({ sessions: [session] })
    expect(html).toContain('입력 필요')
    expect(html).toContain('sample-api')
    expect(html).toContain('나: 실패 case를 추가해 줘')
    expect(html).not.toContain('w1:p1')
  })
  it('opens a session through a real link to its chat route', () => {
    expect(render({ sessions: [session] })).toContain('href="#session=claude%3Aa"')
  })
  it('explains an empty list by the next step', () => {
    expect(render({})).toContain('PC의 Herdr에서 agent를 실행하면 여기에 나타납니다.')
  })
  it('shows neither the empty state nor rows while the first load is pending', () => {
    const html = render({ loaded: false })
    expect(html).not.toContain('agent가 없습니다')
    expect(html).toContain('aria-busy="true"')
  })
  it('says why input is unavailable without internal API names', () => {
    const html = render({ sessions: [session], conditionalInput: 'unsupported' })
    expect(html).toContain('원격 입력')
    expect(html).not.toContain('conditional input')
  })
})
