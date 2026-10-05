import { describe, expect, it } from 'vitest'
import type { Session } from './api'
import { agentName, groupSessions, plainPreview, rowNote, statusPresentation } from './presentation'

const base: Session = { id: 'claude:a', agent: 'claude', pane_id: 'w1:p1', project: '/work/a', title: 'A', status: 'idle', runtime_binding: 'b', chat: true, terminal: true, active: true, lifecycle: 'active' }
const at = (minutes: number) => new Date(Date.parse('2026-09-25T12:00:00Z') - minutes * 60_000).toISOString()

describe('session groups', () => {
  it('orders groups by what needs the reader first and drops empty ones', () => {
    const sessions: Session[] = [
      { ...base, id: 'idle', status: 'idle' },
      { ...base, id: 'working', status: 'working' },
      { ...base, id: 'attention', status: 'needs_attention' },
      { ...base, id: 'lost', status: 'working', lifecycle: 'unverified', runtime_binding: undefined },
    ]
    expect(groupSessions(sessions).map(group => [group.title, group.sessions.map(item => item.id)])).toEqual([
      ['입력 필요', ['attention']], ['작업 중', ['working']], ['대기', ['idle']], ['기타', ['lost']],
    ])
  })
  it('puts the most recent activity first within a group', () => {
    const sessions: Session[] = [
      { ...base, id: 'old', last_activity: at(90) },
      { ...base, id: 'none' },
      { ...base, id: 'new', last_activity: at(2) },
    ]
    expect(groupSessions(sessions)[0].sessions.map(item => item.id)).toEqual(['new', 'old', 'none'])
  })
})

describe('row copy', () => {
  it('reads a Markdown preview as plain text', () => {
    expect(plainPreview('## 원인 차트가 **1만 건**을 넘으면 `pnpm test`로 [설명](https://example.com)을 봅니다')).toBe('원인 차트가 1만 건을 넘으면 pnpm test로 설명을 봅니다')
    expect(plainPreview('배포 checklist - [x] migration dry-run - [ ] rollback 공유 > 참고 ```ts const a = 1 ```')).toBe('배포 checklist migration dry-run rollback 공유 참고 const a = 1')
  })
  it('notes a session that is readable only in the Terminal', () => {
    expect(rowNote({ ...base, chat: false })).toBe('Terminal에서 확인')
    expect(rowNote(base)).toBeNull()
    expect(rowNote({ ...base, chat: false, terminal: false, lifecycle: 'unbound' })).toBeNull()
  })
  it('notes a read-only Chat session', () => {
    expect(rowNote({ ...base, terminal: false, lifecycle: 'unbound' })).toBe('읽기 전용')
  })
})

describe('status presentation', () => {
  it('uses the Herdr status vocabulary for an active session', () => {
    expect(statusPresentation({ status: 'needs_attention', lifecycle: 'active' })).toEqual({ tone: 'attention', label: '입력 필요' })
    expect(statusPresentation({ status: 'working', lifecycle: 'active' })).toEqual({ tone: 'working', label: '작업 중' })
    expect(statusPresentation({ status: 'completed', lifecycle: 'active' })).toEqual({ tone: 'done', label: '완료' })
    expect(statusPresentation({ status: 'idle', lifecycle: 'active' })).toEqual({ tone: 'idle', label: '대기' })
    expect(statusPresentation({ status: 'error', lifecycle: 'active' })).toEqual({ tone: 'unknown', label: '상태 확인 필요' })
  })
  it('lets lifecycle win where the agent cannot be reached from here', () => {
    expect(statusPresentation({ status: 'working', lifecycle: 'unverified' })).toEqual({ tone: 'offline', label: '연결 확인 중' })
    expect(statusPresentation({ status: 'idle', lifecycle: 'unbound' })).toEqual({ tone: 'offline', label: '원격 입력 불가' })
    expect(statusPresentation({ status: 'completed', active: false })).toEqual({ tone: 'offline', label: '종료됨' })
  })
  it('keeps the agent status of a read-only Chat session', () => {
    expect(statusPresentation({ status: 'working', lifecycle: 'unbound', chat: true })).toEqual({ tone: 'working', label: '작업 중' })
  })
  it('keeps the agent status while a pane item hands over to its successor', () => {
    expect(statusPresentation({ status: 'working', lifecycle: 'superseded' })).toEqual({ tone: 'working', label: '작업 중' })
  })
})
describe('agentName', () => {
  it('names known agents and keeps others as reported', () => {
    expect(agentName('claude')).toBe('Claude Code')
    expect(agentName('codex')).toBe('Codex')
    expect(agentName('pi')).toBe('pi')
  })
})
