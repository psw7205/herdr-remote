const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
// A pause this long in a conversation earns a time divider.
const PAUSE = 10 * MINUTE

function calendarDay(at: number, timeZone?: string): number {
  const [year, month, day] = new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(at).split('-').map(Number)
  return Date.UTC(year, month - 1, day) / DAY
}

export function relativeTime(iso: string, now = Date.now(), timeZone?: string): string {
  const at = Date.parse(iso)
  if (Number.isNaN(at)) return ''
  const ago = now - at
  if (ago < MINUTE) return '방금'
  if (ago < HOUR) return `${Math.floor(ago / MINUTE)}분 전`
  if (ago < DAY) return `${Math.floor(ago / HOUR)}시간 전`
  const days = calendarDay(now, timeZone) - calendarDay(at, timeZone)
  if (days <= 1) return '어제'
  if (days < 7) return `${days}일 전`
  return new Intl.DateTimeFormat('ko-KR', { timeZone, month: 'long', day: 'numeric' }).format(at)
}

function clock(at: number, timeZone?: string): string {
  return new Intl.DateTimeFormat('ko-KR', { timeZone, hour: 'numeric', minute: '2-digit' }).format(at)
}

// dividerLabel names the moment a message starts after a pause or on a new day;
// null means the message continues the previous one.
export function dividerLabel(previous: string | null, iso: string, timeZone?: string): string | null {
  const at = Date.parse(iso)
  if (Number.isNaN(at)) return null
  const before = previous ? Date.parse(previous) : Number.NaN
  const sameDay = !Number.isNaN(before) && calendarDay(before, timeZone) === calendarDay(at, timeZone)
  if (sameDay) return at - before >= PAUSE ? clock(at, timeZone) : null
  const date = new Intl.DateTimeFormat('ko-KR', { timeZone, month: 'long', day: 'numeric', weekday: 'short' }).format(at)
  return `${date} ${clock(at, timeZone)}`
}
