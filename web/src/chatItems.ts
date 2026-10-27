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

export function entryKey(entry: ChatEntry): string {
  return entry.kind === 'tools' ? entry.id : entry.message.id
}

export function entryEnd(entry: ChatEntry): string {
  return entry.kind === 'tools' ? entry.tools.at(-1)!.timestamp : entry.message.timestamp
}

const isPrompt = (entry: ChatEntry): entry is { kind: 'message'; message: TextMessage } => entry.kind === 'message' && entry.message.role === 'user'

export type TurnSummary = { duration?: number; answer?: TextMessage }

// turnSummaries maps the key of each finished turn's last entry to its
// summary. A turn runs from a user prompt through the agent's last item before
// the next prompt; the final turn is finished only once the agent is not live.
// The duration spans native transcript timestamps from the prompt to that
// last item; the answer is the turn's last assistant text.
export function turnSummaries(entries: ChatEntry[], live: boolean): Map<string, TurnSummary> {
  const out = new Map<string, TurnSummary>()
  let start = 0
  while (start < entries.length) {
    let end = start + 1
    while (end < entries.length && !isPrompt(entries[end])) end += 1
    const first = entries[start]
    const last = entries[end - 1]
    if (end < entries.length || !live) {
      const summary: TurnSummary = {}
      const duration = isPrompt(first) && last !== first ? Date.parse(entryEnd(last)) - Date.parse(first.message.timestamp) : Number.NaN
      if (Number.isFinite(duration) && duration >= 0) summary.duration = duration
      for (let index = end - 1; index >= start && !summary.answer; index -= 1) {
        const entry = entries[index]
        if (entry.kind === 'message' && entry.message.role === 'assistant' && entry.message.text.trim()) summary.answer = entry.message
      }
      if (summary.duration !== undefined || summary.answer) out.set(entryKey(last), summary)
    }
    start = end
  }
  return out
}
