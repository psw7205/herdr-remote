export type ScrollMetrics = { scrollTop: number; scrollHeight: number; clientHeight: number }

// About three lines of body text: close enough that the reader sees the end.
export const NEAR_BOTTOM_PX = 80
// A fling keeps scrolling after the finger lifts. Each scroll event inside this
// window extends it, so the whole fling counts as the reader's own movement.
export const GESTURE_TAIL_MS = 150

export function isNearBottom({ scrollTop, scrollHeight, clientHeight }: ScrollMetrics, threshold = NEAR_BOTTOM_PX): boolean {
  return scrollHeight - clientHeight - scrollTop <= threshold
}

// Only an upward scroll unpins. A downward scroll that has not reached the
// bottom yet (a smooth scroll in flight) keeps the current state. Inside the
// bottom band an upward move unpins only when it is the reader's gesture: the
// browser also moves scrollTop there on its own (the keyboard closing clamps
// it, shrinking content anchors it), and those must not unpin.
export function nextPinned(pinned: boolean, previousTop: number, metrics: ScrollMetrics, gesture = false): boolean {
  if (gesture && metrics.scrollTop < previousTop) return false
  if (isNearBottom(metrics)) return true
  if (metrics.scrollTop < previousTop) return false
  return pinned
}

// gestureUntil is the time until which scroll events belong to the reader:
// Infinity while a finger is down, a short tail after it lifts or after a wheel
// tick, -Infinity when idle.
export type Follower = { pinned: boolean; previousTop: number; gestureUntil: number }
export const initialFollower: Follower = { pinned: true, previousTop: 0, gestureUntil: -Infinity }

export function gestureStart(follower: Follower): Follower {
  return { ...follower, gestureUntil: Infinity }
}
export function gestureEnd(follower: Follower, now: number): Follower {
  return { ...follower, gestureUntil: now + GESTURE_TAIL_MS }
}
export function inGesture(follower: Follower, now: number): boolean {
  return now <= follower.gestureUntil
}
export function scrolled(follower: Follower, now: number, metrics: ScrollMetrics): Follower {
  const gesture = inGesture(follower, now)
  const pinned = nextPinned(follower.pinned, follower.previousTop, metrics, gesture)
  const gestureUntil = gesture && follower.gestureUntil !== Infinity ? now + GESTURE_TAIL_MS : follower.gestureUntil
  return { pinned, previousTop: metrics.scrollTop, gestureUntil }
}
// Once the gesture is over, a reader who let go inside the bottom band follows again.
export function settled(follower: Follower, metrics: ScrollMetrics): Follower {
  return { ...follower, pinned: follower.pinned || isNearBottom(metrics) }
}
// Snapping to the newest message is deferred while the reader's gesture is in
// progress, so a resize or a new message never pulls the content from under a finger.
export function maySnap(follower: Follower, now: number): boolean {
  return follower.pinned && !inGesture(follower, now)
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
