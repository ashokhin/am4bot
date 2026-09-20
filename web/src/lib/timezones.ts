// The full IANA timezone database the runtime already ships -- no need to
// hand-maintain a list ourselves. Intl.supportedValuesOf('timeZone')
// returns one entry per zone (never both a legacy link name and its
// canonical target for the same zone), so there's nothing to dedupe here.
export interface TimezoneOption {
  value: string
  // "(UTC+04:00) Europe/Moscow" -- offset first, like most sites' pickers,
  // so the list can be sorted by offset instead of alphabetically by
  // region (which tells you nothing about how far a schedule's local
  // time is from anyone else's).
  label: string
  offsetMinutes: number
}

function supportedTimezones(): string[] {
  // Intl.supportedValuesOf is broadly supported in browsers this app
  // otherwise already requires (Radix/Vite's own baseline), but isn't in
  // every TS lib target -- feature-detect rather than assume.
  const intl = Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] }

  if (typeof intl.supportedValuesOf === 'function') {
    try {
      return intl.supportedValuesOf('timeZone')
    } catch {
      // fall through to the fallback below
    }
  }

  // Minimal fallback for a runtime without supportedValuesOf -- UTC plus
  // whatever the browser already reports as the local zone, so the field
  // is never completely empty.
  return Array.from(new Set(['UTC', Intl.DateTimeFormat().resolvedOptions().timeZone]))
}

// The current UTC offset for zone, in minutes (DST-aware, evaluated
// "now" -- an offset picker showing today's actual offset is what every
// site does; a handful of zones will read differently in six months
// across a DST transition, which is expected, not a bug).
function offsetMinutes(zone: string, at: Date): number {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'longOffset' }).formatToParts(at)
  const raw = parts.find((p) => p.type === 'timeZoneName')?.value ?? 'GMT+00:00'
  const match = /GMT([+-]\d{2}):(\d{2})/.exec(raw)

  if (!match) {
    return 0
  }

  const sign = match[1].startsWith('-') ? -1 : 1
  const hours = Math.abs(Number(match[1]))
  const minutes = Number(match[2])

  return sign * (hours * 60 + minutes)
}

function formatOffset(minutes: number): string {
  const sign = minutes < 0 ? '-' : '+'
  const abs = Math.abs(minutes)
  const hh = String(Math.floor(abs / 60)).padStart(2, '0')
  const mm = String(abs % 60).padStart(2, '0')

  return `UTC${sign}${hh}:${mm}`
}

export function getTimezoneOptions(): TimezoneOption[] {
  const now = new Date()

  return supportedTimezones()
    .map((value) => {
      const offset = offsetMinutes(value, now)

      return { value, offsetMinutes: offset, label: `(${formatOffset(offset)}) ${value}` }
    })
    .sort((a, b) => a.offsetMinutes - b.offsetMinutes || a.value.localeCompare(b.value))
}
