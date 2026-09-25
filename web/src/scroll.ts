export type ScrollMetrics = { scrollTop: number; scrollHeight: number; clientHeight: number }

// About three lines of body text: close enough that the reader sees the end.
export const NEAR_BOTTOM_PX = 80

export function isNearBottom({ scrollTop, scrollHeight, clientHeight }: ScrollMetrics, threshold = NEAR_BOTTOM_PX): boolean {
  return scrollHeight - clientHeight - scrollTop <= threshold
}

// Only an upward scroll unpins. A downward scroll that has not reached the
// bottom yet (a smooth scroll in flight) keeps the current state.
export function nextPinned(pinned: boolean, previousTop: number, metrics: ScrollMetrics): boolean {
  if (isNearBottom(metrics)) return true
  if (metrics.scrollTop < previousTop) return false
  return pinned
}

export type Follow = { pinned: boolean; unseen: number }

// added is the change in message count. A reader away from the bottom keeps
// their place and sees a count instead; a shorter history (a fresh snapshot)
// resets that count rather than inventing unseen messages.
export function afterMessages({ pinned, unseen }: Follow, added: number): { follow: boolean; unseen: number } {
  if (pinned) return { follow: true, unseen: 0 }
  if (added < 0) return { follow: false, unseen: 0 }
  return { follow: false, unseen: unseen + added }
}
