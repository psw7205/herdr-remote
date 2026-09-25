import { useLayoutEffect, type RefObject } from 'react'
import { keyboardViewport } from './terminalSizing'

// Pins the app shell to the visual viewport so every screen keeps its header
// and its bottom controls visible above the on-screen keyboard. Android
// Chrome also resizes the layout itself (viewport meta interactive-widget);
// iOS Safari only shrinks the visual viewport. Pinch zoom is not tracked.
export function useVisualViewportShell(shell: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const viewport = window.visualViewport
    const element = shell.current
    if (!viewport || !element) return
    const update = () => {
      const visible = keyboardViewport(viewport)
      if (!visible) return
      element.style.setProperty('--app-height', `${visible.height}px`)
      element.style.setProperty('--app-top', `${visible.top}px`)
    }
    update()
    viewport.addEventListener('resize', update)
    viewport.addEventListener('scroll', update)
    return () => { viewport.removeEventListener('resize', update); viewport.removeEventListener('scroll', update) }
  }, [shell])
}
