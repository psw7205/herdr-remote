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

// An uncertain delivery can only be checked where the pane is visible: the
// Terminal view when the session has one, otherwise Herdr on the PC.
export function pendingCommandCopy(terminal: boolean) {
  const place = terminal ? 'Terminal' : 'PC의 Herdr'
  return {
    blocked: `이전 입력이 전달됐는지 아직 모릅니다. ${place}에서 확인한 뒤 새로 입력하세요.`,
    action: `${place}에서 확인 후 새로 입력`,
    cleared: `${place}에서 확인했다면 새로 입력할 수 있습니다.`,
  }
}

export function rejectedMessage(code?: string): string {
  switch (code) {
    case 'SESSION_CHANGED':
    case 'RUNTIME_BINDING_MISMATCH':
      return '세션이 바뀌어 전달하지 않았습니다. 목록에서 다시 여세요.'
    case 'HERDR_UNSUPPORTED':
      return '이 Herdr에는 원격 입력 기능이 없어 전달하지 않았습니다. PC에서 Herdr patch를 확인하세요.'
    default:
      return `전달이 거부됐습니다 (${code ?? '알 수 없음'}).`
  }
}
