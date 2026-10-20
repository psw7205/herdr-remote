import { afterEach, describe, expect, it, vi } from 'vitest'
import { startSession, type Session, type StartCandidate } from './api'
import { chooseStart, findStartedSession, groupCandidates, placementLabel, startOutcome } from './sessionStart'

const closed: StartCandidate = { id: 'c1', name: 'sample-api', root: 'work', open: false, placement: 'new_workspace' }
const open: StartCandidate = { id: 'c2', name: 'docs-site', root: 'work', open: true, workspace: 'docs', placement: 'new_tab' }
const outside: StartCandidate = { id: 'c3', name: 'scratch', open: true, workspace: 'scratch', placement: 'new_tab' }

afterEach(() => vi.unstubAllGlobals())

describe('session start request', () => {
  it('sends only the opaque candidate, kind and placement', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ status: 'accepted', created: { workspace_id: 'w2', tab_id: 'w2:t1', pane_id: 'w2:p1', agent: 'mobile-a' } }), { status: 202 }))
    vi.stubGlobal('fetch', fetch)
    const result = await startSession('c1', 'claude', 'new_workspace', 'command-1')
    expect(result.created?.pane_id).toBe('w2:p1')
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('/api/sessions')
    expect(JSON.parse(init.body)).toEqual({ command_id: 'command-1', command_type: 'session_start', payload: { candidate_id: 'c1', kind: 'claude', placement: 'new_workspace' } })
    expect(init.body).not.toContain('/')
  })
  it('treats an error answered before any receipt as a rejection', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'ORIGIN_REJECTED' }), { status: 403 })))
    expect(await startSession('c1', 'claude', 'new_workspace', 'command-1')).toEqual({ status: 'rejected', code: 'ORIGIN_REJECTED' })
  })
  it('lets a lost answer surface as an error instead of a result', async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError('network lost'))
    vi.stubGlobal('fetch', fetch)
    await expect(startSession('c1', 'claude', 'new_workspace', 'command-1')).rejects.toThrow('network lost')
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})

describe('start intent', () => {
  it('keeps the command ID for the same folder and refuses another while uncertain', () => {
    const first = chooseStart(null, closed, 'claude', () => 'id-1')
    expect(first).toEqual({ id: 'id-1', candidate: 'c1', placement: 'new_workspace', kind: 'claude' })
    expect(chooseStart(first, closed, 'claude', () => 'id-2')).toBe(first)
    expect(chooseStart(first, open, 'claude', () => 'id-2')).toBeNull()
    // The folder opened in the meantime: a tab is a different request.
    expect(chooseStart(first, { ...closed, open: true, placement: 'new_tab' }, 'claude', () => 'id-2')).toBeNull()
  })
})

describe('start outcome copy', () => {
  const created = { workspace_id: 'w2', tab_id: 'w2:t1', pane_id: 'w2:p1', agent: 'mobile-a' }
  it('opens the new session by its pane whenever Herdr accepted the start', () => {
    expect(startOutcome({ status: 'accepted', created })).toMatchObject({ tone: 'info', settled: true, pane: 'w2:p1' })
    expect(startOutcome({ status: 'accepted', code: 'AGENT_NOT_READY', created })).toMatchObject({ settled: true, pane: 'w2:p1' })
    expect(startOutcome({ status: 'accepted', code: 'AGENT_START_UNCONFIRMED', created })).toMatchObject({ tone: 'info', settled: true, pane: 'w2:p1' })
    expect(startOutcome({ status: 'rejected', code: 'AGENT_START_FAILED', created }).pane).toBeUndefined()
  })
  it('shows a trust screen as a step at the PC, not a failure', () => {
    const outcome = startOutcome({ status: 'accepted', code: 'AGENT_NOT_READY', created })
    expect(outcome.text).toContain('PC의 Herdr에서 확인하면')
    expect(outcome.tone).toBe('warning')
    expect(outcome.settled).toBe(true)
  })
  it('does not warn about an unconfirmed start: the session screen shows readiness', () => {
    const outcome = startOutcome({ status: 'accepted', code: 'AGENT_START_UNCONFIRMED', created })
    expect(outcome.text).not.toContain('확인하지 못했습니다')
    expect(outcome.tone).toBe('info')
  })
  it('never settles an uncertain delivery', () => {
    const outcome = startOutcome({ status: 'delivery_unknown', code: 'DELIVERY_UNKNOWN' })
    expect(outcome.settled).toBe(false)
    expect(outcome.text).toContain('자동으로 다시 보내지 않습니다')
  })
  it('says a created tab stays open when the agent failed to start', () => {
    const outcome = startOutcome({ status: 'rejected', code: 'AGENT_START_FAILED', created: { workspace_id: 'w1', tab_id: 'w1:t3', pane_id: 'w1:p3', agent: 'mobile-a' } })
    expect(outcome.text).toContain('자동으로 닫지 않으니')
    expect(outcome.tone).toBe('danger')
  })
  it('explains a folder that changed after listing', () => {
    expect(startOutcome({ status: 'rejected', code: 'CANDIDATE_CHANGED' }).text).toContain('폴더 상태가 바뀌어')
  })
})

describe('finding the started session', () => {
  const session = (id: string, pane: string): Session => ({ id, agent: 'claude', pane_id: pane, project: '/work/a', title: '', status: 'idle', chat: true, terminal: true, active: true })
  const list = (...pages: Session[][]) => {
    const fn = vi.fn()
    for (const page of pages) fn.mockResolvedValueOnce({ sessions: page, start: { kinds: ['claude'], new_workspace: true }, bridge: null })
    return fn
  }
  afterEach(() => vi.useRealTimers())
  it('finds the session by pane whether it is listed as pane: or claude:', async () => {
    vi.useFakeTimers()
    const pages = list([session('claude:other', 'w1:p1')], [session('pane:w2:p1', 'w2:p1')])
    const found = findStartedSession('w2:p1', pages, { attempts: 3, interval: 3000 })
    await vi.advanceTimersByTimeAsync(3000)
    expect((await found)?.id).toBe('pane:w2:p1')
    expect(pages).toHaveBeenCalledTimes(2)
    expect((await findStartedSession('w2:p1', list([session('claude:new', 'w2:p1')]), { attempts: 1 }))?.id).toBe('claude:new')
  })
  it('gives up after the attempts and tolerates a failed poll', async () => {
    vi.useFakeTimers()
    const pages = vi.fn().mockRejectedValueOnce(new TypeError('network lost')).mockResolvedValue({ sessions: [], start: { kinds: [], new_workspace: false }, bridge: null })
    const result = findStartedSession('w2:p1', pages, { attempts: 3, interval: 1000 })
    await vi.advanceTimersByTimeAsync(5000)
    expect(await result).toBeNull()
    expect(pages).toHaveBeenCalledTimes(3)
  })
  it('stops when aborted', async () => {
    vi.useFakeTimers()
    const controller = new AbortController()
    const pages = list([], [session('pane:w2:p1', 'w2:p1')])
    const result = findStartedSession('w2:p1', pages, { attempts: 3, interval: 3000, signal: controller.signal })
    await vi.advanceTimersByTimeAsync(10)
    controller.abort()
    await vi.advanceTimersByTimeAsync(3000)
    expect(await result).toBeNull()
    expect(pages).toHaveBeenCalledTimes(1)
  })
})

describe('candidate groups', () => {
  it('lists open workspaces first, then folders under their root', () => {
    const groups = groupCandidates([closed, open, outside])
    expect(groups.map(group => [group.title, group.candidates.map(item => item.id)])).toEqual([['열린 workspace', ['c2', 'c3']], ['work', ['c1']]])
  })
  it('labels an open workspace as a new tab there', () => {
    expect(placementLabel(open)).toBe('docs에 새 tab')
    expect(placementLabel(closed)).toBe('새 workspace')
  })
})
