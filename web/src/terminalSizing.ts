// Client-side render grid for a visible Herdr frame. Nothing here resizes the
// Herdr PTY; the grid only has to be large enough that no frame line wraps.

export type GridSize = { cols: number; rows: number }

const ANSI = /\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[P^_][^\x1b]*\x1b\\|\x1b[()*+][0-~]|\x1b[ -/]*[0-~]/g

// Cell widths of xterm.js's default Unicode V6 provider, ported from
// @xterm/xterm 6.0.0 `src/common/input/UnicodeV6.ts` (MIT, (c) 2019 the
// xterm.js authors). The grid must match what xterm lays out: an under-count
// wraps a frame line, so this follows xterm exactly instead of a newer Unicode
// table. Emoji such as U+1F600 are therefore 1 cell, as xterm renders them
// without the unicode11 addon.
const BMP_COMBINING: ReadonlyArray<readonly [number, number]> = [
  [0x0300, 0x036F], [0x0483, 0x0486], [0x0488, 0x0489],
  [0x0591, 0x05BD], [0x05BF, 0x05BF], [0x05C1, 0x05C2],
  [0x05C4, 0x05C5], [0x05C7, 0x05C7], [0x0600, 0x0603],
  [0x0610, 0x0615], [0x064B, 0x065E], [0x0670, 0x0670],
  [0x06D6, 0x06E4], [0x06E7, 0x06E8], [0x06EA, 0x06ED],
  [0x070F, 0x070F], [0x0711, 0x0711], [0x0730, 0x074A],
  [0x07A6, 0x07B0], [0x07EB, 0x07F3], [0x0901, 0x0902],
  [0x093C, 0x093C], [0x0941, 0x0948], [0x094D, 0x094D],
  [0x0951, 0x0954], [0x0962, 0x0963], [0x0981, 0x0981],
  [0x09BC, 0x09BC], [0x09C1, 0x09C4], [0x09CD, 0x09CD],
  [0x09E2, 0x09E3], [0x0A01, 0x0A02], [0x0A3C, 0x0A3C],
  [0x0A41, 0x0A42], [0x0A47, 0x0A48], [0x0A4B, 0x0A4D],
  [0x0A70, 0x0A71], [0x0A81, 0x0A82], [0x0ABC, 0x0ABC],
  [0x0AC1, 0x0AC5], [0x0AC7, 0x0AC8], [0x0ACD, 0x0ACD],
  [0x0AE2, 0x0AE3], [0x0B01, 0x0B01], [0x0B3C, 0x0B3C],
  [0x0B3F, 0x0B3F], [0x0B41, 0x0B43], [0x0B4D, 0x0B4D],
  [0x0B56, 0x0B56], [0x0B82, 0x0B82], [0x0BC0, 0x0BC0],
  [0x0BCD, 0x0BCD], [0x0C3E, 0x0C40], [0x0C46, 0x0C48],
  [0x0C4A, 0x0C4D], [0x0C55, 0x0C56], [0x0CBC, 0x0CBC],
  [0x0CBF, 0x0CBF], [0x0CC6, 0x0CC6], [0x0CCC, 0x0CCD],
  [0x0CE2, 0x0CE3], [0x0D41, 0x0D43], [0x0D4D, 0x0D4D],
  [0x0DCA, 0x0DCA], [0x0DD2, 0x0DD4], [0x0DD6, 0x0DD6],
  [0x0E31, 0x0E31], [0x0E34, 0x0E3A], [0x0E47, 0x0E4E],
  [0x0EB1, 0x0EB1], [0x0EB4, 0x0EB9], [0x0EBB, 0x0EBC],
  [0x0EC8, 0x0ECD], [0x0F18, 0x0F19], [0x0F35, 0x0F35],
  [0x0F37, 0x0F37], [0x0F39, 0x0F39], [0x0F71, 0x0F7E],
  [0x0F80, 0x0F84], [0x0F86, 0x0F87], [0x0F90, 0x0F97],
  [0x0F99, 0x0FBC], [0x0FC6, 0x0FC6], [0x102D, 0x1030],
  [0x1032, 0x1032], [0x1036, 0x1037], [0x1039, 0x1039],
  [0x1058, 0x1059], [0x1160, 0x11FF], [0x135F, 0x135F],
  [0x1712, 0x1714], [0x1732, 0x1734], [0x1752, 0x1753],
  [0x1772, 0x1773], [0x17B4, 0x17B5], [0x17B7, 0x17BD],
  [0x17C6, 0x17C6], [0x17C9, 0x17D3], [0x17DD, 0x17DD],
  [0x180B, 0x180D], [0x18A9, 0x18A9], [0x1920, 0x1922],
  [0x1927, 0x1928], [0x1932, 0x1932], [0x1939, 0x193B],
  [0x1A17, 0x1A18], [0x1B00, 0x1B03], [0x1B34, 0x1B34],
  [0x1B36, 0x1B3A], [0x1B3C, 0x1B3C], [0x1B42, 0x1B42],
  [0x1B6B, 0x1B73], [0x1DC0, 0x1DCA], [0x1DFE, 0x1DFF],
  [0x200B, 0x200F], [0x202A, 0x202E], [0x2060, 0x2063],
  [0x206A, 0x206F], [0x20D0, 0x20EF], [0x302A, 0x302F],
  [0x3099, 0x309A], [0xA806, 0xA806], [0xA80B, 0xA80B],
  [0xA825, 0xA826], [0xFB1E, 0xFB1E], [0xFE00, 0xFE0F],
  [0xFE20, 0xFE23], [0xFEFF, 0xFEFF], [0xFFF9, 0xFFFB]
];
const HIGH_COMBINING: ReadonlyArray<readonly [number, number]> = [
  [0x10A01, 0x10A03], [0x10A05, 0x10A06], [0x10A0C, 0x10A0F],
  [0x10A38, 0x10A3A], [0x10A3F, 0x10A3F], [0x1D167, 0x1D169],
  [0x1D173, 0x1D182], [0x1D185, 0x1D18B], [0x1D1AA, 0x1D1AD],
  [0x1D242, 0x1D244], [0xE0001, 0xE0001], [0xE0020, 0xE007F],
  [0xE0100, 0xE01EF]
];

let bmpTable: Uint8Array | undefined

function buildBmpTable(): Uint8Array {
  const table = new Uint8Array(65536)
  table.fill(1)
  table[0] = 0
  table.fill(0, 1, 32)
  table.fill(0, 0x7f, 0xa0)
  table.fill(2, 0x1100, 0x1160)
  table[0x2329] = 2
  table[0x232a] = 2
  table.fill(2, 0x2e80, 0xa4d0)
  table[0x303f] = 1
  table.fill(2, 0xac00, 0xd7a4)
  table.fill(2, 0xf900, 0xfb00)
  table.fill(2, 0xfe10, 0xfe1a)
  table.fill(2, 0xfe30, 0xfe70)
  table.fill(2, 0xff00, 0xff61)
  table.fill(2, 0xffe0, 0xffe7)
  // Combining last, so it overrides the wide ranges it overlaps.
  for (const [from, to] of BMP_COMBINING) table.fill(0, from, to + 1)
  return table
}

function inRanges(code: number, ranges: ReadonlyArray<readonly [number, number]>): boolean {
  let low = 0
  let high = ranges.length - 1
  while (low <= high) {
    const mid = (low + high) >> 1
    if (code > ranges[mid][1]) low = mid + 1
    else if (code < ranges[mid][0]) high = mid - 1
    else return true
  }
  return false
}

export function cellWidth(code: number): number {
  if (code < 32) return 0
  if (code < 127) return 1
  if (code < 65536) return (bmpTable ??= buildBmpTable())[code]
  if (inRanges(code, HIGH_COMBINING)) return 0
  if ((code >= 0x20000 && code <= 0x2fffd) || (code >= 0x30000 && code <= 0x3fffd)) return 2
  return 1
}

// Iterating by code point matches xterm's getStringCellWidth for V6: a valid
// surrogate pair is one code point, a lone surrogate is one BMP cell, and a
// combining mark adds no width to the cell it joins.
export function displayWidth(line: string): number {
  let width = 0
  for (const ch of line) width += cellWidth(ch.codePointAt(0) ?? 0)
  return width
}

// Measures a visible ANSI frame: its line count and its widest line in cells.
export function frameDimensions(text: string): GridSize {
  const lines = text.replace(ANSI, '').split(/\r?\n/)
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop()
  let cols = 0
  for (const line of lines) cols = Math.max(cols, displayWidth(line.replace(/\r/g, '')))
  return { cols, rows: text === '' ? 0 : lines.length }
}

const MIN_GRID: GridSize = { cols: 20, rows: 5 }
const MAX_GRID: GridSize = { cols: 1000, rows: 500 }

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value))

// The Bridge hint is the pane's layout rect, an upper bound on the PTY grid in
// normal layouts. The frame's own size is a floor for cases the hint misses
// (direct attach size locks, older Bridges). Without a hint the grid only grows
// while the view is open, so short frames do not make the font jump.
export function gridSize(hint: Partial<GridSize>, frame: GridSize, previous?: GridSize): GridSize {
  const hinted = (hint.cols ?? 0) > 0 && (hint.rows ?? 0) > 0
  const base = hinted ? { cols: hint.cols!, rows: hint.rows! } : previous ?? MIN_GRID
  return {
    cols: clamp(Math.max(base.cols, frame.cols), MIN_GRID.cols, MAX_GRID.cols),
    rows: clamp(Math.max(base.rows, frame.rows), MIN_GRID.rows, MAX_GRID.rows),
  }
}

export const MIN_FONT_SIZE = 6
export const MAX_FIT_FONT_SIZE = 16
export const MAX_FONT_SIZE = 32
export const DEFAULT_CELL_RATIO = 0.6

export type FitInput = {
  availableWidth: number
  cols: number
  // Rendered cell width divided by font size, measured from xterm when possible.
  cellRatio: number
  // User zoom multiplier; 1 means fit to width.
  zoom?: number
}

// Font size that fits `cols` cells into `availableWidth`. Below MIN_FONT_SIZE
// the terminal keeps the minimum and the container scrolls horizontally.
export function fitFontSize({ availableWidth, cols, cellRatio, zoom = 1 }: FitInput): number {
  const ratio = cellRatio > 0 ? cellRatio : DEFAULT_CELL_RATIO
  const fit = availableWidth > 0 && cols > 0 ? availableWidth / (cols * ratio) : MAX_FIT_FONT_SIZE
  const fitted = clamp(fit, MIN_FONT_SIZE, MAX_FIT_FONT_SIZE)
  return clamp(Math.floor(fitted * zoom * 4) / 4, MIN_FONT_SIZE, MAX_FONT_SIZE)
}

export const ZOOM_STEPS = [0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3] as const

export function stepZoom(zoom: number, direction: 1 | -1): number {
  const index = ZOOM_STEPS.findIndex(step => step >= zoom)
  const current = index < 0 ? ZOOM_STEPS.length - 1 : index
  return ZOOM_STEPS[clamp(current + direction, 0, ZOOM_STEPS.length - 1)]
}
