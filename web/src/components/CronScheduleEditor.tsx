import { PlusIcon, XIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '../lib/utils'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'

interface Props {
  value: string[]
  onChange: (next: string[]) => void
}

type Mode = 'interval' | 'specific'

interface FriendlyEntry {
  mode: Mode
  /** 0 (Sun) - 6 (Sat), at least one. */
  days: number[]
  /** Minutes, for mode 'interval'. */
  intervalMinutes: number
  /** Hours 0-23, for mode 'specific' -- cron cross-multiplies this with `minutes` below. */
  hours: number[]
  /** Minutes 0-55 (step 5), for mode 'specific'. */
  minutes: number[]
}

/** Every value this UI itself ever produces -- used to offer only these. */
const INTERVAL_OPTIONS = [5, 10, 15, 20, 30, 45, 60]
const MINUTE_OPTIONS = [0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55]
const HOUR_OPTIONS = Array.from({ length: 24 }, (_, i) => i)
const DAY_OPTIONS = [0, 1, 2, 3, 4, 5, 6]
const WEEKDAYS = [1, 2, 3, 4, 5]
const WEEKENDS = [0, 6]

function sameSet(a: number[], b: number[]): boolean {
  return a.length === b.length && a.every((v) => b.includes(v))
}

/** Parses a cron field like "1-5", "1,3,5", "*", or "7" into a sorted, deduped number list -- undefined if anything doesn't fit. */
function parseNumberField(field: string, min: number, max: number): number[] | undefined {
  if (field === '*') {
    return Array.from({ length: max - min + 1 }, (_, i) => min + i)
  }

  const out = new Set<number>()

  for (const part of field.split(',')) {
    const range = /^(\d+)-(\d+)$/.exec(part)

    if (range) {
      const [, a, b] = range
      const lo = Number(a)
      const hi = Number(b)
      if (lo < min || hi > max || lo > hi) return undefined
      for (let n = lo; n <= hi; n++) out.add(n)
      continue
    }

    if (!/^\d+$/.test(part)) return undefined
    const n = Number(part)
    if (n < min || n > max) return undefined
    out.add(n)
  }

  return out.size === 0 ? undefined : Array.from(out).sort((a, b) => a - b)
}

function formatNumberField(values: number[], allValues: number[]): string {
  return sameSet(values, allValues) ? '*' : [...values].sort((a, b) => a - b).join(',')
}

function entryToCron(entry: FriendlyEntry): string {
  const dow = formatNumberField(entry.days, DAY_OPTIONS)

  if (entry.mode === 'interval') {
    return `*/${entry.intervalMinutes} * * * ${dow}`
  }

  const hour = formatNumberField(entry.hours, HOUR_OPTIONS)
  const minute = formatNumberField(entry.minutes, MINUTE_OPTIONS)
  return `${minute} ${hour} * * ${dow}`
}

/**
 * Parses a raw cron string this editor itself could have produced (or an
 * equivalent hand-written one -- ranges and lists both parse fine) back
 * into a FriendlyEntry. Anything else returns undefined -- the caller
 * falls back to showing the raw expression in an "advanced" text field
 * instead of silently reinterpreting (and possibly corrupting) it.
 */
function cronToEntry(raw: string): FriendlyEntry | undefined {
  const fields = raw.trim().split(/\s+/)
  if (fields.length !== 5) {
    return undefined
  }

  const [minute, hour, dom, month, dow] = fields
  if (dom !== '*' || month !== '*') {
    return undefined
  }

  const days = parseNumberField(dow, 0, 6)
  if (!days) {
    return undefined
  }

  const intervalMatch = /^\*\/(\d+)$/.exec(minute)
  if (intervalMatch && hour === '*') {
    const minutes = Number(intervalMatch[1])
    if (INTERVAL_OPTIONS.includes(minutes)) {
      return { mode: 'interval', days, intervalMinutes: minutes, hours: HOUR_OPTIONS, minutes: MINUTE_OPTIONS }
    }
    return undefined
  }

  const hours = parseNumberField(hour, 0, 23)
  // A literal "*" here means "every minute", which is NOT the same as this
  // editor's own "every value on the 5-minute grid" -- parseNumberField's
  // generic '*' handling would expand it to all 60 raw minutes (1, 2, 3,
  // ...), almost none of which are in MINUTE_OPTIONS, tripping the
  // every() check below and wrongly falling back to the raw text field.
  // Map it directly to "every grid value selected" instead, matching what
  // entryToCron's formatNumberField(minutes, MINUTE_OPTIONS) itself
  // produces when all of MINUTE_OPTIONS is selected.
  const minutes = minute === '*' ? MINUTE_OPTIONS : parseNumberField(minute, 0, 59)
  if (!hours || !minutes || !minutes.every((m) => MINUTE_OPTIONS.includes(m))) {
    return undefined
  }

  return { mode: 'specific', days, intervalMinutes: 5, hours, minutes }
}

const DEFAULT_ENTRY: FriendlyEntry = { mode: 'interval', days: DAY_OPTIONS, intervalMinutes: 5, hours: HOUR_OPTIONS, minutes: MINUTE_OPTIONS }

function toggle(values: number[], v: number): number[] {
  // Never let the last one go -- an entry with zero days/hours/minutes
  // selected has no valid cron representation.
  if (values.includes(v)) {
    return values.length === 1 ? values : values.filter((x) => x !== v)
  }

  return [...values, v].sort((a, b) => a - b)
}

function ToggleChip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex h-7 min-w-7 items-center justify-center rounded-md border px-1.5 text-xs font-medium transition-colors',
        active
          ? 'border-primary bg-primary text-primary-foreground'
          : 'border-input bg-transparent text-muted-foreground hover:bg-accent hover:text-accent-foreground',
      )}
    >
      {children}
    </button>
  )
}

/**
 * Friendly schedule builder: each entry picks any combination of days of
 * the week, and either "every N minutes" or one or more specific
 * hour/minute combinations (cron cross-multiplies the two lists, so
 * hours=[6,7,12] + minutes=[20,50] fires at 6:20, 6:50, 7:20, 7:50, 12:20,
 * 12:50) -- covers real-world schedules like "twice an hour during peak
 * hours on weekdays, once an hour the rest of the week" without anyone
 * needing to know cron syntax. An entry that doesn't fit this shape
 * (hand-edited, a minute not on the 5-minute grid, ...) falls back to a
 * raw, still-editable cron text field rather than being silently
 * reinterpreted.
 */
export function CronScheduleEditor({ value, onChange }: Props) {
  const { t } = useTranslation()

  function updateEntry(index: number, entry: FriendlyEntry) {
    const next = value.slice()
    next[index] = entryToCron(entry)
    onChange(next)
  }

  function updateRaw(index: number, raw: string) {
    const next = value.slice()
    next[index] = raw
    onChange(next)
  }

  function removeAt(index: number) {
    onChange(value.filter((_, i) => i !== index))
  }

  function add() {
    onChange([...value, entryToCron(DEFAULT_ENTRY)])
  }

  return (
    <div className="flex flex-col gap-3">
      <Label>{t('nodes.schedules.label')}</Label>
      {value.map((raw, i) => {
        const entry = cronToEntry(raw)

        return (
          <div key={i} className="flex flex-col gap-3 rounded-md border p-3">
            {entry ? (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-xs text-muted-foreground">{t('nodes.schedules.dayScope')}</span>
                  <div className="flex gap-1">
                    {DAY_OPTIONS.map((d) => (
                      <ToggleChip key={d} active={entry.days.includes(d)} onClick={() => updateEntry(i, { ...entry, days: toggle(entry.days, d) })}>
                        {t(`nodes.schedules.dayAbbrev.${d}`)}
                      </ToggleChip>
                    ))}
                  </div>
                  <div className="ml-1 flex gap-1">
                    <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => updateEntry(i, { ...entry, days: DAY_OPTIONS })}>
                      {t('nodes.schedules.dayScopeDaily')}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => updateEntry(i, { ...entry, days: WEEKDAYS })}>
                      {t('nodes.schedules.dayScopeWeekdays')}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => updateEntry(i, { ...entry, days: WEEKENDS })}>
                      {t('nodes.schedules.dayScopeWeekends')}
                    </Button>
                  </div>

                  <Select value={entry.mode} onValueChange={(v) => updateEntry(i, { ...entry, mode: v as Mode })}>
                    <SelectTrigger className="ml-auto w-44" aria-label={t('nodes.schedules.mode')}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="interval">{t('nodes.schedules.modeInterval')}</SelectItem>
                      <SelectItem value="specific">{t('nodes.schedules.modeSpecific')}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {entry.mode === 'interval' ? (
                  <Select value={String(entry.intervalMinutes)} onValueChange={(v) => updateEntry(i, { ...entry, intervalMinutes: Number(v) })}>
                    <SelectTrigger className="w-44" aria-label={t('nodes.schedules.everyMinutes')}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {INTERVAL_OPTIONS.map((n) => (
                        <SelectItem key={n} value={String(n)}>
                          {t('nodes.schedules.everyMinutesValue', { count: n })}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <div className="flex flex-col gap-2">
                    <div className="flex flex-wrap items-start gap-2">
                      <span className="mt-1 w-14 shrink-0 text-xs text-muted-foreground">{t('nodes.schedules.hours')}</span>
                      <div className="flex flex-wrap gap-1">
                        {HOUR_OPTIONS.map((h) => (
                          <ToggleChip key={h} active={entry.hours.includes(h)} onClick={() => updateEntry(i, { ...entry, hours: toggle(entry.hours, h) })}>
                            {h}
                          </ToggleChip>
                        ))}
                      </div>
                    </div>
                    <div className="flex flex-wrap items-start gap-2">
                      <span className="mt-1 w-14 shrink-0 text-xs text-muted-foreground">{t('nodes.schedules.minutes')}</span>
                      <div className="flex flex-wrap gap-1">
                        {MINUTE_OPTIONS.map((m) => (
                          <ToggleChip key={m} active={entry.minutes.includes(m)} onClick={() => updateEntry(i, { ...entry, minutes: toggle(entry.minutes, m) })}>
                            {String(m).padStart(2, '0')}
                          </ToggleChip>
                        ))}
                      </div>
                    </div>
                    <p className="text-xs text-muted-foreground">{t('nodes.schedules.specificHint')}</p>
                  </div>
                )}
              </>
            ) : (
              <div className="flex flex-1 flex-col gap-1">
                <Input
                  type="text"
                  value={raw}
                  onChange={(e) => updateRaw(i, e.target.value)}
                  className="max-w-72 font-mono"
                  aria-label={t('nodes.schedules.entryLabel', { index: i + 1 })}
                />
                <p className="text-xs text-muted-foreground">{t('nodes.schedules.advancedHint')}</p>
              </div>
            )}

            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-fit"
              onClick={() => removeAt(i)}
              aria-label={t('nodes.schedules.remove', { index: i + 1 })}
            >
              <XIcon className="size-4" /> {t('nodes.schedules.remove', { index: i + 1 })}
            </Button>
          </div>
        )
      })}
      <Button type="button" variant="outline" size="sm" className="w-fit" onClick={add}>
        <PlusIcon /> {t('nodes.schedules.add')}
      </Button>
      <p className="text-xs text-muted-foreground">{t('nodes.schedules.hint')}</p>
    </div>
  )
}
