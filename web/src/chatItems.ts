import type { Message, Session, ToolActivity } from './api'

export type TextMessage = Message & { role: 'user' | 'assistant' }
export type ToolMessage = Message & { role: 'tool'; tool: ToolActivity }
export type ChatEntry =
  | { kind: 'message'; message: TextMessage }
  | { kind: 'tools'; id: string; tools: ToolMessage[] }

const isText = (message: Message): message is TextMessage => message.role === 'user' || message.role === 'assistant'
const isTool = (message: Message): message is ToolMessage => message.role === 'tool' && typeof message.tool === 'object' && message.tool !== null

// applyMessageEvent returns the list after one live event, or null for an
// event that does not touch the list. An update for an item this client does
// not hold is dropped; the next snapshot carries it.
export function applyMessageEvent(messages: Message[], type: string, payload: unknown): Message[] | null {
  const next = payload as Message
  if (!next || typeof next.id !== 'string') return null
  if (type === 'message.user' || type === 'message.assistant' || type === 'message.tool') {
    return messages.some(item => item.id === next.id) ? messages : [...messages, next]
  }
  if (type === 'message.updated') {
    const index = messages.findIndex(item => item.id === next.id)
    if (index < 0) return messages
    const out = messages.slice()
    out[index] = next
    return out
  }
  return null
}

// chatEntries groups consecutive tool items between text messages. Roles this
// client does not know are skipped.
export function chatEntries(messages: Message[]): ChatEntry[] {
  const out: ChatEntry[] = []
  for (const message of messages) {
    if (isText(message)) { out.push({ kind: 'message', message }); continue }
    if (!isTool(message)) continue
    const last = out.at(-1)
    if (last?.kind === 'tools') last.tools.push(message)
    else out.push({ kind: 'tools', id: message.id, tools: [message] })
  }
  return out
}

export function textCount(messages: Message[]): number {
  let count = 0
  for (const message of messages) if (isText(message)) count += 1
  return count
}

export type ToolState = ToolActivity['state']

// A call without a result runs only while the agent can still finish it: the
// session is live and Herdr reports it working or waiting for an answer.
// Otherwise it is shown as having no result. The Bridge marks it unknown only
// once the transcript moves on, which an ended session never does.
export function toolsLive(session: Pick<Session, 'status' | 'active'> & Partial<Pick<Session, 'lifecycle'>>): boolean {
  return session.active && session.lifecycle !== 'ended' && session.lifecycle !== 'superseded' && (session.status === 'working' || session.status === 'needs_attention')
}

export function toolState(tool: ToolActivity, live: boolean): ToolState {
  return tool.state === 'running' && !live ? 'unknown' : tool.state
}
