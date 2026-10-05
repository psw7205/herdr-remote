import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { Message, ToolActivity } from './api'
import { applyMessageEvent, chatEntries, textCount, toolState, toolsLive, type ToolMessage } from './chatItems'
import { COLLAPSE_AFTER, ToolDetail, ToolGroup } from './ToolActivity'

const text = (id: string, role: 'user' | 'assistant'): Message => ({ id, role, text: id, timestamp: '2026-10-05T00:00:00Z' })
const call = (id: string, tool: Partial<ToolActivity> = {}): ToolMessage => ({
  id, role: 'tool', text: '', timestamp: '2026-10-05T00:00:00Z',
  tool: { id: `toolu_${id}`, name: 'Bash', summary: `echo ${id}`, state: 'completed', input: '{\n  "command": "echo"\n}', input_truncated: false, result: 'ok', result_truncated: false, ...tool },
})

describe('message events', () => {
  it('appends new items once, including tool items', () => {
    const start = [text('u1', 'user')]
    const withTool = applyMessageEvent(start, 'message.tool', call('t1', { state: 'running' }))!
    expect(withTool.map(item => item.id)).toEqual(['u1', 't1'])
    expect(applyMessageEvent(withTool, 'message.tool', call('t1'))).toBe(withTool)
    expect(applyMessageEvent(withTool, 'message.assistant', text('a1', 'assistant'))!.map(item => item.id)).toEqual(['u1', 't1', 'a1'])
  })
  it('replaces an updated item in place and keeps the order', () => {
    const list = [text('u1', 'user'), call('t1', { state: 'running' }), text('a1', 'assistant')]
    const next = applyMessageEvent(list, 'message.updated', call('t1', { state: 'error', result: 'boom' }))!
    expect(next.map(item => item.id)).toEqual(['u1', 't1', 'a1'])
    expect(next[1].tool).toMatchObject({ state: 'error', result: 'boom' })
    expect(list[1].tool?.state).toBe('running')
  })
  it('drops an update for an item it does not hold and ignores other events', () => {
    const list = [text('u1', 'user')]
    expect(applyMessageEvent(list, 'message.updated', call('missing'))).toBe(list)
    expect(applyMessageEvent(list, 'message.future', text('x', 'assistant'))).toBeNull()
    expect(applyMessageEvent(list, 'message.tool', null)).toBeNull()
  })
})

describe('chat entries', () => {
  it('groups consecutive tool items between text messages', () => {
    const entries = chatEntries([text('u1', 'user'), call('t1'), call('t2'), text('a1', 'assistant'), call('t3')])
    expect(entries.map(entry => entry.kind === 'tools' ? entry.tools.map(item => item.id).join('+') : entry.message.id)).toEqual(['u1', 't1+t2', 'a1', 't3'])
  })
  it('skips roles it does not know and tool items without detail', () => {
    const future = { id: 'f1', role: 'future', text: 'x', timestamp: '' } as unknown as Message
    const bare = { id: 't0', role: 'tool', text: '', timestamp: '' } as Message
    const entries = chatEntries([text('u1', 'user'), future, bare, call('t1'), text('a1', 'assistant')])
    expect(entries.map(entry => entry.kind)).toEqual(['message', 'tools', 'message'])
    expect(textCount([text('u1', 'user'), future, call('t1')])).toBe(1)
  })
  it('shows a result-less call as running only while the agent can finish it', () => {
    const running = call('t1', { state: 'running' }).tool
    expect(toolState(running, toolsLive({ active: true, status: 'working', lifecycle: 'active' }))).toBe('running')
    expect(toolState(running, toolsLive({ active: true, status: 'needs_attention', lifecycle: 'unverified' }))).toBe('running')
    expect(toolState(running, toolsLive({ active: true, status: 'idle', lifecycle: 'active' }))).toBe('unknown')
    expect(toolState(running, toolsLive({ active: false, status: 'completed', lifecycle: 'ended' }))).toBe('unknown')
    expect(toolState(call('t2', { state: 'error' }).tool, false)).toBe('error')
  })
})

describe('tool group', () => {
  it('lists a short group as one muted row per call', () => {
    const html = renderToStaticMarkup(<ToolGroup tools={[call('t1'), call('t2', { name: 'Read', summary: 'src/a.ts' })]} live />)
    expect(html).not.toContain('도구 2개</span>')
    expect(html.match(/aria-expanded="false"/g)).toHaveLength(2)
    expect(html).toContain('>Read<')
    expect(html).toContain('>src/a.ts<')
    expect(html).toContain('>완료<')
  })
  it('collapses a long group to its newest call behind a count', () => {
    const tools = Array.from({ length: COLLAPSE_AFTER + 2 }, (_, index) => call(`t${index}`, index === 1 ? { state: 'error' } : {}))
    const html = renderToStaticMarkup(<ToolGroup tools={tools} live={false} />)
    expect(html).toContain(`도구 ${tools.length}개`)
    expect(html).toContain('오류 1')
    expect(html).toMatch(/<button type="button"[^>]*aria-expanded="false"[^>]*aria-controls="[^"]+"/)
    expect(html).toContain(`echo t${tools.length - 1}`)
    expect(html).not.toContain('echo t0<')
  })
  it('marks a running call for screen readers', () => {
    expect(renderToStaticMarkup(<ToolGroup tools={[call('t1', { state: 'running' })]} live />)).toContain('>실행 중<')
  })
})

describe('tool detail', () => {
  it('shows input and result as preformatted text with a truncation note', () => {
    const html = renderToStaticMarkup(<ToolDetail tool={call('t1', { result: 'line 1\nline 2', result_truncated: true }).tool} state="completed" />)
    expect(html).toContain('<pre')
    expect(html).toContain('&quot;command&quot;')
    expect(html).toContain('line 1\nline 2')
    expect(html).toContain('앞부분만 표시합니다')
  })
  it('labels an error result and explains a missing one', () => {
    expect(renderToStaticMarkup(<ToolDetail tool={call('t1', { state: 'error', result: 'denied' }).tool} state="error" />)).toContain('>오류<')
    const unknown = renderToStaticMarkup(<ToolDetail tool={call('t1', { state: 'running', result: '' }).tool} state="unknown" />)
    expect(unknown).toContain('결과가 기록되지 않았습니다.')
    expect(renderToStaticMarkup(<ToolDetail tool={call('t1', { input: '' }).tool} state="completed" />)).toContain('입력이 없습니다.')
  })
})
