import { afterEach, describe, expect, it, vi } from 'vitest'
import { sendCommand, eventURL, listSessions, sessionCardText, type Session } from './api'

const session: Session = { id: 'claude:native-a', agent: 'claude', pane_id: 'w1:p2', project: 'repo', title: 'Task', status: 'idle', runtime_binding: 'bound-a', chat: true, terminal: true, active: true }
afterEach(() => vi.unstubAllGlobals())
describe('client command delivery', () => {
  it('keeps one command ID and never retries after an uncertain network failure', async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError('network lost'))
    vi.stubGlobal('fetch', fetch)
    await expect(sendCommand(session, 'prompt', 'hello', 'command-1')).rejects.toThrow('network lost')
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({command_id:'command-1',session_id:session.id,runtime_binding:'bound-a',command_type:'prompt',payload:{text:'hello'}})
  })
  it('uses an epoch cursor for replay after the snapshot', () => {
    vi.stubGlobal('location', new URL('https://private.ts.net/'))
    expect(eventURL(session.id, {epoch:'new-epoch',sequence:12})).toBe('wss://private.ts.net/api/sessions/claude%3Anative-a/events?epoch=new-epoch&sequence=12')
  })
})
describe('session list', () => {
  const respond = (body: unknown) => vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body))))
  it('reads the Herdr conditional input capability', async () => {
    respond({sessions:[session],herdr:{conditional_input:'unsupported'}})
    expect(await listSessions()).toEqual({sessions:[session],conditionalInput:'unsupported'})
  })
  it('treats a missing capability from an older Bridge as unknown', async () => {
    respond({sessions:[session]})
    expect(await listSessions()).toEqual({sessions:[session],conditionalInput:'unknown'})
  })
  it('describes card availability from chat and terminal', () => {
    expect(sessionCardText({chat:true,terminal:true})).toBe('기존 대화 연결됨')
    expect(sessionCardText({chat:false,terminal:true})).toBe('Terminal에서 확인 가능')
    expect(sessionCardText({chat:false,terminal:false})).toBe('입력·Terminal 사용 불가')
  })
})
