import { describe, expect, it } from 'vitest'
import { cellWidth, displayWidth, fitFontSize, frameDimensions, gridSize, keyboardViewport, MAX_FONT_SIZE, MIN_FONT_SIZE, stepZoom, type GridSize } from './terminalSizing'

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

  it('strips DCS, charset and other escape sequences', () => {
    expect(frameDimensions('\x1bPq#0;2;0;0;0\x1b\\abc')).toEqual({ cols: 3, rows: 1 })
    expect(frameDimensions('\x1b(0lqk\x1b(B ok')).toEqual({ cols: 6, rows: 1 })
    expect(frameDimensions('\x1b_app\x1b\\\x1b^pm\x1b\\\x1b7ab\x1b8\x1b=')).toEqual({ cols: 2, rows: 1 })
  })
})

// Expected values are xterm 6.0.0 Unicode V6 widths (the default provider).
describe('cellWidth', () => {
  it.each([
    ['NUL', 0x0, 0], ['control', 0x1b, 0], ['ASCII', 0x41, 1], ['DEL', 0x7f, 0], ['C1', 0x9f, 0], ['NBSP', 0xa0, 1],
    ['combining acute', 0x301, 0], ['Hangul choseong', 0x1100, 2], ['Hangul jungseong', 0x1161, 0], ['Hangul jongseong', 0x11ff, 0],
    ['zero width space', 0x200b, 0], ['word joiner', 0x2060, 0], ['combining enclosing', 0x20dd, 0],
    ['left angle bracket', 0x2329, 2], ['right angle bracket', 0x232a, 2], ['box drawing', 0x2500, 1],
    ['CJK radical', 0x2e80, 2], ['ideographic combining', 0x302a, 0], ['half fill space', 0x303f, 1], ['kana voicing', 0x3099, 0],
    ['CJK ideograph', 0x4e00, 2], ['Yi', 0xa4cf, 2], ['after Yi', 0xa4d0, 1], ['Hangul syllable', 0xac00, 2], ['last syllable', 0xd7a3, 2],
    ['after syllables', 0xd7a4, 1], ['lone surrogate', 0xd800, 1], ['compatibility ideograph', 0xf900, 2], ['after compatibility', 0xfb00, 1],
    ['variation selector', 0xfe0f, 0], ['vertical comma', 0xfe10, 2], ['vertical ellipsis', 0xfe19, 2], ['after vertical forms', 0xfe1a, 1],
    ['combining half mark', 0xfe20, 0], ['CJK compatibility form', 0xfe30, 2], ['small comma', 0xfe50, 2], ['small ampersand', 0xfe6b, 2],
    ['after small forms', 0xfe70, 1], ['BOM', 0xfeff, 0], ['fullwidth A', 0xff21, 2], ['halfwidth katakana', 0xff61, 1],
    ['fullwidth cent', 0xffe0, 2], ['after fullwidth signs', 0xffe7, 1], ['interlinear anchor', 0xfff9, 0],
    ['emoji', 0x1f600, 1], ['musical combining', 0x1d167, 0], ['CJK extension B', 0x20000, 2], ['plane 2 end', 0x2fffd, 2],
    ['plane 2 noncharacter', 0x2fffe, 1], ['CJK extension G', 0x30000, 2], ['tag', 0xe0041, 0], ['variation selector supplement', 0xe0100, 0],
  ])('%s U+%s', (_name, code, width) => {
    expect(cellWidth(code)).toBe(width)
  })
})

describe('displayWidth', () => {
  it.each([
    ['surrogate pair CJK', '\u{20000}x', 3],
    ['surrogate pair emoji', 'a\u{1f600}b', 3],
    ['combining mark joins', 'e\u0301x', 2],
    ['combining on wide', '가\u0301', 2],
    ['decomposed Hangul', '\u1100\u1161\u11a8', 2],
    ['lone high surrogate', 'a\ud800', 2],
    ['lone low surrogate', '\udc00b', 2],
    ['small form variants', '\ufe50\ufe51', 4],
  ])('%s', (_name, text, width) => {
    expect(displayWidth(text)).toBe(width)
  })
})

describe('gridSize', () => {
  it('uses the Bridge pane size when the frame fits in it', () => {
    expect(gridSize({ cols: 161, rows: 45 }, { cols: 120, rows: 45 }).grid).toEqual({ cols: 161, rows: 45 })
  })

  it('grows past the hint so no frame line wraps or scrolls off', () => {
    expect(gridSize({ cols: 80, rows: 24 }, { cols: 100, rows: 30 }).grid).toEqual({ cols: 100, rows: 30 })
  })

  it('follows a smaller hint when the desktop pane shrinks', () => {
    expect(gridSize({ cols: 90, rows: 30 }, { cols: 40, rows: 30 }, [{ cols: 161, rows: 45 }]).grid).toEqual({ cols: 90, rows: 30 })
  })

  it('uses the largest of the recent frames without a hint', () => {
    let recent: GridSize[] = []
    const poll = (frame: GridSize) => { const next = gridSize({}, frame, recent); recent = next.recent; return next.grid }
    expect(poll({ cols: 128, rows: 40 })).toEqual({ cols: 128, rows: 40 })
    for (let i = 0; i < 4; i++) expect(poll({ cols: 30, rows: 10 })).toEqual({ cols: 128, rows: 40 })
    expect(poll({ cols: 30, rows: 10 })).toEqual({ cols: 30, rows: 10 })
    expect(recent).toHaveLength(5)
    expect(gridSize({}, { cols: 3, rows: 1 }).grid).toEqual({ cols: 20, rows: 5 })
  })

  it('keeps a hinted size through a missed layout read', () => {
    const hinted = gridSize({ cols: 161, rows: 45 }, { cols: 120, rows: 40 })
    expect(gridSize({ cols: 0, rows: 0 }, { cols: 120, rows: 40 }, hinted.recent).grid).toEqual({ cols: 161, rows: 45 })
  })

  it('ignores recent frames when the hint is present', () => {
    const wide = gridSize({}, { cols: 200, rows: 60 })
    expect(gridSize({ cols: 90, rows: 30 }, { cols: 40, rows: 30 }, wide.recent).grid).toEqual({ cols: 90, rows: 30 })
  })
})

describe('keyboardViewport', () => {
  it('follows the keyboard and ignores pinch zoom', () => {
    expect(keyboardViewport({ height: 420, offsetTop: 0, scale: 1 })).toEqual({ height: 420, top: 0 })
    expect(keyboardViewport({ height: 300, offsetTop: 120, scale: 2 })).toBeUndefined()
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

  it('clamps a zoomed size at the maximum', () => {
    expect(fitFontSize({ availableWidth: 1000, cols: 40, cellRatio: 0.6, zoom: 3 })).toBe(MAX_FONT_SIZE)
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
