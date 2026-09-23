export type PendingCommand = { id: string; binding: string; text: string }

// A retry of the same user intent must preserve its command ID. Changing the
// binding or text while delivery is uncertain requires an explicit new intent.
export function chooseCommand(
  pending: PendingCommand | null,
  binding: string,
  text: string,
  newID: () => string,
): PendingCommand | null {
  if (pending) return pending.binding === binding && pending.text === text ? pending : null
  return { id: newID(), binding, text }
}

export function readPending(key: string): PendingCommand | null {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(key) ?? 'null')
    if (value && typeof value === 'object' && 'id' in value && 'binding' in value && 'text' in value && typeof value.id === 'string' && typeof value.binding === 'string' && typeof value.text === 'string') return value as PendingCommand
  } catch { /* A corrupt browser draft cannot authorize a new command. */ }
  return null
}
