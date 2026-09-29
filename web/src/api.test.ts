import { afterEach, describe, expect, it, vi } from 'vitest'
import { composerHint, lifecycleNotice, sendCommand, eventURL, listSessions, sessionLifecycle, successorOf, supersededBy, type Session } from './api'

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
    expect(await listSessions()).toEqual({sessions:[session],conditionalInput:'unsupported',start:{kinds:[],new_workspace:false}})
  })
  it('treats a missing capability from an older Bridge as unknown', async () => {
    respond({sessions:[session]})
    expect(await listSessions()).toEqual({sessions:[session],conditionalInput:'unknown',start:{kinds:[],new_workspace:false}})
  })
  it('never describes an unverified session as connected or ended', () => {
    const unverified: Session = { ...session, runtime_binding: undefined, terminal: false, lifecycle: 'unverified' }
    expect(lifecycleNotice(unverified)).toContain('계속 실행 중')
    expect(composerHint(unverified)).toBe('agent 연결을 확인할 수 없어 입력할 수 없습니다.')
    expect(composerHint(unverified)).not.toContain('종료')
  })
  it('keeps the ended copy only for an ended session', () => {
    const ended: Session = { ...session, active: false, lifecycle: 'ended' }
    expect(composerHint(ended)).toBe('종료된 세션에는 입력할 수 없습니다.')
    expect(lifecycleNotice(ended)).toBeNull()
    expect(lifecycleNotice({ ...session, lifecycle: 'active' })).toBeNull()
  })
  it('shows a never-bound pane item as unavailable without the recovery notice', () => {
    const unbound: Session = { ...session, id: 'pane:w1:p2', runtime_binding: undefined, chat: false, terminal: false, lifecycle: 'unbound' }
    expect(lifecycleNotice(unbound)).toBeNull()
    expect(composerHint(unbound)).not.toContain('종료')
  })
  it('never describes a superseded pane item as ended', () => {
    const superseded: Session = { ...session, id: 'pane:w1:p2', active: false, lifecycle: 'superseded', successor_id: 'claude:native-a' }
    expect(successorOf(superseded)).toBe('claude:native-a')
    expect(successorOf({ lifecycle: 'ended', successor_id: 'claude:native-a' })).toBeNull()
    expect(lifecycleNotice(superseded)).toBeNull()
    expect(composerHint(superseded)).not.toContain('종료')
  })
  it('tells a reader of a working session to wait, not to open the Terminal', () => {
    expect(composerHint({ ...session, status: 'working' })).toBe('작업 중입니다. 끝나면 이어서 입력할 수 있습니다.')
  })
  it('sends a waiting prompt to where it can be answered', () => {
    expect(composerHint({ ...session, status: 'needs_attention' })).toBe('입력을 기다리고 있습니다. Terminal에서 응답하세요.')
    expect(composerHint({ ...session, status: 'needs_attention', terminal: false })).toBe('입력을 기다리고 있습니다. PC의 Herdr에서 응답하세요.')
  })
  it('explains an unbound session by what it cannot do', () => {
    const unbound: Session = { ...session, id: 'pane:w1:p2', runtime_binding: undefined, chat: false, terminal: false, lifecycle: 'unbound' }
    expect(composerHint(unbound)).toBe('이 세션은 원격 입력을 지원하지 않습니다. PC의 Herdr에서 입력하세요.')
  })
  it('derives lifecycle from active for an older Bridge', () => {
    expect(sessionLifecycle({active:true})).toBe('active')
    expect(sessionLifecycle({active:false})).toBe('ended')
  })
})
describe('superseded session', () => {
  const successor: Session = { ...session, id: 'claude:native-a', runtime_binding: 'bound-b' }
  const detail = (body: Partial<Session>) => vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => new Response(JSON.stringify({ session: { ...session, id: 'pane:w1:p2', active: false, ...body }, snapshot: { cursor: { epoch: 'e', sequence: 0 }, data: [] } }))))
  it('follows the listed successor with its own binding', async () => {
    detail({ lifecycle: 'superseded', successor_id: successor.id, runtime_binding: undefined })
    const next = await supersededBy('pane:w1:p2', [successor])
    expect(next).toEqual(successor)
    expect(next?.runtime_binding).toBe('bound-b')
  })
  it('stays put for a listed, ended, unknown or unlisted-successor session', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    expect(await supersededBy(successor.id, [successor])).toBeNull()
    expect(await supersededBy(null, [successor])).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
    detail({ lifecycle: 'ended' })
    expect(await supersededBy('pane:w1:p2', [successor])).toBeNull()
    detail({ lifecycle: 'superseded', successor_id: 'claude:other' })
    expect(await supersededBy('pane:w1:p2', [successor])).toBeNull()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'session not found' }), { status: 404 })))
    expect(await supersededBy('pane:w1:p2', [successor])).toBeNull()
  })
  it('stops fetching an ended session until it is listed again', async () => {
    const ended = new Set<string>()
    detail({ lifecycle: 'ended' })
    const fetch = vi.mocked(globalThis.fetch)
    expect(await supersededBy('pane:w1:p2', [successor], ended)).toBeNull()
    expect(await supersededBy('pane:w1:p2', [successor], ended)).toBeNull()
    expect(fetch).toHaveBeenCalledTimes(1)
    const revived: Session = { ...session, id: 'pane:w1:p2' }
    expect(await supersededBy('pane:w1:p2', [revived, successor], ended)).toBeNull()
    detail({ lifecycle: 'superseded', successor_id: successor.id })
    expect(await supersededBy('pane:w1:p2', [successor], ended)).toEqual(successor)
  })
  it('keeps polling while the successor is not listed yet or the fetch fails', async () => {
    const ended = new Set<string>()
    detail({ lifecycle: 'superseded', successor_id: successor.id })
    expect(await supersededBy('pane:w1:p2', [], ended)).toBeNull()
    expect(await supersededBy('pane:w1:p2', [successor], ended)).toEqual(successor)
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network lost')))
    expect(await supersededBy('pane:w1:p2', [successor], ended)).toBeNull()
    expect(ended.size).toBe(0)
  })
})
