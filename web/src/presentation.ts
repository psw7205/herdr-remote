import { sessionLifecycle, type Session } from './api'

export type Tone = 'attention' | 'working' | 'done' | 'idle' | 'unknown' | 'offline'
export type StatusPresentation = { tone: Tone; label: string }

// The Herdr TUI vocabulary: blocked, working, done (not seen yet), idle.
const byStatus: Record<Session['status'], StatusPresentation> = {
  needs_attention: { tone: 'attention', label: '입력 필요' },
  working: { tone: 'working', label: '작업 중' },
  completed: { tone: 'done', label: '완료' },
  idle: { tone: 'idle', label: '대기' },
  error: { tone: 'unknown', label: '상태 확인 필요' },
}

// Lifecycle wins where this client cannot act on the agent; a superseded pane
// item keeps its agent status while the view moves to the successor.
// An unbound item with Chat (a read-only codex: session) still shows what
// the agent is doing; only its input is closed.
export function statusPresentation(session: Pick<Session, 'status'> & Partial<Pick<Session, 'lifecycle' | 'active' | 'chat'>>): StatusPresentation {
  switch (sessionLifecycle({ active: session.active ?? false, lifecycle: session.lifecycle })) {
    case 'unverified': return { tone: 'offline', label: '연결 확인 중' }
    case 'unbound': return session.chat ? byStatus[session.status] ?? byStatus.error : { tone: 'offline', label: '원격 입력 불가' }
    case 'ended': return { tone: 'offline', label: '종료됨' }
    default: return byStatus[session.status] ?? byStatus.error
  }
}

export type SessionGroup = { key: string; title: string; sessions: Session[] }
const groupOrder: { key: string; title: string; tones: Tone[] }[] = [
  { key: 'attention', title: '입력 필요', tones: ['attention'] },
  { key: 'working', title: '작업 중', tones: ['working'] },
  { key: 'done', title: '완료', tones: ['done'] },
  { key: 'idle', title: '대기', tones: ['idle'] },
  { key: 'other', title: '기타', tones: ['unknown', 'offline'] },
]

// Groups follow what the reader has to do first; within a group the most
// recent activity leads, and sessions without one keep the Bridge order.
export function groupSessions(sessions: Session[]): SessionGroup[] {
  const recent = (session: Session) => Date.parse(session.last_activity ?? '') || 0
  return groupOrder
    .map(({ key, title, tones }) => ({ key, title, sessions: sessions.filter(item => tones.includes(statusPresentation(item).tone)).sort((a, b) => recent(b) - recent(a)) }))
    .filter(group => group.sessions.length > 0)
}

// The Bridge sends raw Markdown cut to one line; the list shows its words.
export function plainPreview(text: string): string {
  return text
    .replace(/```[\w#+.-]*/g, ' ')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/(\*\*|__)(.+?)\1/g, '$2')
    .replace(/(^|\s)(?:#{1,6}|>|[-*+](?: \[[ xX]\])?)(?=\s)/g, '$1')
    .replace(/\s+/g, ' ')
    .trim()
}

export function rowNote(session: Pick<Session, 'chat' | 'terminal'> & Partial<Pick<Session, 'lifecycle' | 'active'>>): string | null {
  const lifecycle = sessionLifecycle({ active: session.active ?? false, lifecycle: session.lifecycle })
  if (lifecycle === 'unbound' && session.chat) return '읽기 전용'
  return lifecycle === 'active' && !session.chat && session.terminal ? 'Terminal에서 확인' : null
}

const agentNames: Record<string, string> = { claude: 'Claude Code', codex: 'Codex' }
export function agentName(agent: string): string {
  return agentNames[agent] ?? agent
}
export function projectName(project: string): string {
  return project.split('/').filter(Boolean).at(-1) ?? project
}
