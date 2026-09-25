import { describe, expect, it } from 'vitest'
import { displayWidth, fitFontSize, frameDimensions, gridSize, MIN_FONT_SIZE, stepZoom } from './terminalSizing'

describe('frameDimensions', () => {
  it('counts rows and widest line without ANSI escapes', () => {
    const text = '\x1b[32mready\x1b[0m\r\n\x1b]8;;https://example.test\x07link\x1b]8;;\x07 abcdef\r\n\x1b[1;31m\x1b[0m\r\n'
    expect(frameDimensions(text)).toEqual({ cols: 11, rows: 3 })
  })

  it('counts wide characters as two cells', () => {
    expect(displayWidth('한글 ok')).toBe(7)
    expect(frameDimensions('a\n日本')).toEqual({ cols: 4, rows: 2 })
  })

  it('treats an empty frame as zero size', () => {
    expect(frameDimensions('')).toEqual({ cols: 0, rows: 0 })
  })
})

describe('gridSize', () => {
  it('uses the Bridge pane size when the frame fits in it', () => {
    expect(gridSize({ cols: 161, rows: 45 }, { cols: 120, rows: 45 })).toEqual({ cols: 161, rows: 45 })
  })

  it('grows past the hint so no frame line wraps or scrolls off', () => {
    expect(gridSize({ cols: 80, rows: 24 }, { cols: 100, rows: 30 })).toEqual({ cols: 100, rows: 30 })
  })

  it('follows a smaller hint when the desktop pane shrinks', () => {
    expect(gridSize({ cols: 90, rows: 30 }, { cols: 40, rows: 30 }, { cols: 161, rows: 45 })).toEqual({ cols: 90, rows: 30 })
  })

  it('falls back to a grow-only frame size without a hint', () => {
    const first = gridSize({}, { cols: 128, rows: 40 })
    expect(first).toEqual({ cols: 128, rows: 40 })
    expect(gridSize({ cols: 0, rows: 0 }, { cols: 30, rows: 10 }, first)).toEqual({ cols: 128, rows: 40 })
    expect(gridSize({}, { cols: 3, rows: 1 })).toEqual({ cols: 20, rows: 5 })
  })
})

describe('fitFontSize', () => {
  it('fits all columns into the container width', () => {
    const size = fitFontSize({ availableWidth: 374, cols: 60, cellRatio: 0.6 })
    expect(size).toBe(10.25)
    expect(size * 0.6 * 60).toBeLessThanOrEqual(374)
  })

  it('stops at the minimum so wide panes scroll horizontally', () => {
    expect(fitFontSize({ availableWidth: 374, cols: 161, cellRatio: 0.6 })).toBe(MIN_FONT_SIZE)
  })

  it('caps the fitted size for narrow panes and applies zoom', () => {
    expect(fitFontSize({ availableWidth: 1000, cols: 40, cellRatio: 0.6 })).toBe(16)
    expect(fitFontSize({ availableWidth: 374, cols: 60, cellRatio: 0.6, zoom: 2 })).toBe(20.75)
    expect(fitFontSize({ availableWidth: 374, cols: 60, cellRatio: 0.6, zoom: 0.5 })).toBe(MIN_FONT_SIZE)
  })

  it('uses the default ratio for an unmeasured renderer', () => {
    expect(fitFontSize({ availableWidth: 374, cols: 60, cellRatio: 0 })).toBe(10.25)
  })
})

describe('stepZoom', () => {
  it('moves between zoom steps and stays in range', () => {
    expect(stepZoom(1, 1)).toBe(1.25)
    expect(stepZoom(1, -1)).toBe(0.75)
    expect(stepZoom(0.5, -1)).toBe(0.5)
    expect(stepZoom(3, 1)).toBe(3)
  })
})
