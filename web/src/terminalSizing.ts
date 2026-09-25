// Client-side render grid for a visible Herdr frame. Nothing here resizes the
// Herdr PTY; the grid only has to be large enough that no frame line wraps.

export type GridSize = { cols: number; rows: number }

const ANSI = /\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[P^_][^\x1b]*\x1b\\|\x1b[()*+][0-~]|\x1b[ -/]*[0-~]/g

// Approximate terminal cell width. Over-counting only leaves blank cells, so
// ambiguous wide ranges (CJK, Hangul, emoji) count as 2.
function cellWidth(code: number): number {
  if (code < 0x20 || (code >= 0x7f && code < 0xa0)) return 0
  if ((code >= 0x300 && code <= 0x36f) || (code >= 0x200b && code <= 0x200f) || (code >= 0xfe00 && code <= 0xfe0f)) return 0
  if (
    (code >= 0x1100 && code <= 0x115f) || (code >= 0x2e80 && code <= 0xa4cf) || (code >= 0xac00 && code <= 0xd7a3) ||
    (code >= 0xf900 && code <= 0xfaff) || (code >= 0xfe30 && code <= 0xfe4f) || (code >= 0xff00 && code <= 0xff60) ||
    (code >= 0xffe0 && code <= 0xffe6) || (code >= 0x1f300 && code <= 0x1faff) || (code >= 0x20000 && code <= 0x3fffd)
  ) return 2
  return 1
}

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
