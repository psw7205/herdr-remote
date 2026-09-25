import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { afterMessages, isNearBottom, nextPinned } from './scroll'

// Keeps a chat scroller at the newest message while the reader is there, and
// leaves the reader's place alone (counting new messages) once they scroll up.
export function useStickToBottom(scroller: RefObject<HTMLElement | null>, content: RefObject<HTMLElement | null>, count: number) {
  const pinned = useRef(true)
  const lastTop = useRef(0)
  const previous = useRef(0)
  const [away, setAway] = useState(false)
  const [unseen, setUnseen] = useState(0)

  const toBottom = useCallback((smooth = false) => {
    const element = scroller.current
    pinned.current = true
    setAway(false)
    setUnseen(0)
    element?.scrollTo({ top: element.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
  }, [scroller])

  useLayoutEffect(() => {
    const added = count - previous.current
    const first = previous.current === 0 && count > 0
    previous.current = count
    if (first) { toBottom(); return }
    const next = afterMessages({ pinned: pinned.current, unseen: 0 }, added)
    if (next.follow) toBottom()
    else setUnseen(current => afterMessages({ pinned: false, unseen: current }, added).unseen)
  }, [count, toBottom])

  useEffect(() => {
    const element = scroller.current
    if (!element) return
    const onScroll = () => {
      pinned.current = nextPinned(pinned.current, lastTop.current, element)
      lastTop.current = element.scrollTop
      setAway(!pinned.current)
      if (pinned.current) setUnseen(0)
    }
    // Keyboard, rotation and Markdown reflow change sizes without a scroll
    // event; a pinned reader stays at the newest message through them.
    const observer = new ResizeObserver(() => { if (pinned.current) element.scrollTop = element.scrollHeight })
    observer.observe(element)
    if (content.current) observer.observe(content.current)
    element.addEventListener('scroll', onScroll, { passive: true })
    return () => { observer.disconnect(); element.removeEventListener('scroll', onScroll) }
  }, [scroller, content])

  return { away, unseen, toBottom }
}
