import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { StartSession } from './StartSession'
import { SessionList } from './SessionList'

describe('start session screen', () => {
  it('states that the permission mode follows the PC shell and is not guaranteed', () => {
    const html = renderToStaticMarkup(<StartSession start={{ kinds: ['claude'], new_workspace: true }} onBack={() => {}} />)
    expect(html).toContain('PC 사용자 shell 설정')
    expect(html).toContain('보장하지 않습니다')
    expect(html).toContain('폴더를 고르세요')
    expect(html).toContain('disabled')
  })
})

describe('session list start entry', () => {
  const render = (canStart: boolean) => renderToStaticMarkup(<SessionList sessions={[]} loaded error="" conditionalInput="supported" canStart={canStart} onStart={() => {}} onOpen={() => {}} />)
  it('offers a new session only when the Bridge allows a kind', () => {
    expect(render(true)).toContain('aria-label="새 세션 시작"')
    expect(render(false)).not.toContain('새 세션 시작')
  })
})
