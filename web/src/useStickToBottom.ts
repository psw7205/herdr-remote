import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { afterMessages, GESTURE_TAIL_MS, gestureEnd, gestureStart, initialFollower, maySnap, scrolled, settled, type Follower } from './scroll'

// Keeps a chat scroller at the newest message while the reader is there, and
// leaves the reader's place alone (counting new messages) once they scroll up.
export function useStickToBottom(scroller: RefObject<HTMLElement | null>, content: RefObject<HTMLElement | null>, count: number) {
  const follower = useRef<Follower>(initialFollower)
  const previous = useRef(0)
  const [away, setAway] = useState(false)
  const [unseen, setUnseen] = useState(0)

  const toBottom = useCallback((smooth = false) => {
    const element = scroller.current
    follower.current = { ...follower.current, pinned: true }
    setAway(false)
    setUnseen(0)
    element?.scrollTo({ top: element.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
  }, [scroller])

  useLayoutEffect(() => {
    const added = count - previous.current
    const first = previous.current === 0 && count > 0
    previous.current = count
    if (first) { toBottom(); return }
    const next = afterMessages({ pinned: follower.current.pinned, unseen: 0 }, added)
    if (!next.follow) { setUnseen(current => afterMessages({ pinned: false, unseen: current }, added).unseen); return }
    // A pinned reader whose finger is still down is caught up when the gesture settles.
    if (maySnap(follower.current, performance.now())) toBottom()
  }, [count, toBottom])

  useEffect(() => {
    const element = scroller.current
    if (!element) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const publish = () => {
      const { pinned } = follower.current
      setAway(!pinned)
      if (pinned) setUnseen(0)
    }
    const snap = () => { element.scrollTop = element.scrollHeight }
    const settle = () => {
      const now = performance.now()
      const until = follower.current.gestureUntil
      if (until === Infinity) return
      if (now <= until) { timer = setTimeout(settle, until - now + 1); return }
      follower.current = settled(follower.current, element)
      publish()
      if (follower.current.pinned) snap()
    }
    const scheduleSettle = () => { clearTimeout(timer); timer = setTimeout(settle, GESTURE_TAIL_MS + 1) }
    const onScroll = () => { follower.current = scrolled(follower.current, performance.now(), element); publish() }
    const onTouchStart = () => { clearTimeout(timer); follower.current = gestureStart(follower.current) }
    const onTouchEnd = (event: TouchEvent) => {
      if (event.touches.length > 0) return
      follower.current = gestureEnd(follower.current, performance.now())
      scheduleSettle()
    }
    const onWheel = () => { follower.current = gestureEnd(follower.current, performance.now()); scheduleSettle() }
    // Keyboard, rotation and Markdown reflow change sizes without a scroll
    // event; a pinned reader stays at the newest message through them.
    const observer = new ResizeObserver(() => { if (maySnap(follower.current, performance.now())) snap() })
    observer.observe(element)
    if (content.current) observer.observe(content.current)
    element.addEventListener('scroll', onScroll, { passive: true })
    element.addEventListener('touchstart', onTouchStart, { passive: true })
    element.addEventListener('touchend', onTouchEnd, { passive: true })
    element.addEventListener('touchcancel', onTouchEnd, { passive: true })
    element.addEventListener('wheel', onWheel, { passive: true })
    return () => {
      clearTimeout(timer)
      observer.disconnect()
      element.removeEventListener('scroll', onScroll)
      element.removeEventListener('touchstart', onTouchStart)
      element.removeEventListener('touchend', onTouchEnd)
      element.removeEventListener('touchcancel', onTouchEnd)
      element.removeEventListener('wheel', onWheel)
    }
  }, [scroller, content])

  return { away, unseen, toBottom }
}
