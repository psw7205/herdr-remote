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
    blocked: `이전 입력의 전달 여부를 ${place}에서 확인한 뒤 새 입력을 작성하세요.`,
    action: `${place} 확인 후 새 입력`,
    cleared: `${place}에서 이전 입력을 확인한 후 새 입력을 작성할 수 있습니다.`,
  }
}

export function rejectedMessage(code?: string): string {
  switch (code) {
    case 'SESSION_CHANGED':
    case 'RUNTIME_BINDING_MISMATCH':
      return '세션이 바뀌었습니다. 목록에서 다시 선택하세요.'
    case 'HERDR_UNSUPPORTED':
      return '현재 Herdr에 conditional input API가 없어 입력을 전달하지 않았습니다. PC에서 Herdr patch 적용 여부를 확인하세요.'
    default:
      return `전달 거부: ${code ?? '확인 필요'}`
  }
}
