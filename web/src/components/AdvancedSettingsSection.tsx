import { ChevronDownIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { NodeExtraConfig } from '../api/types'
import { cn } from '../lib/utils'
import { InfoHint } from './InfoHint'
import { Badge } from './ui/badge'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Switch } from './ui/switch'

const CATERING_DURATION_HOURS = ['6', '12', '18', '24', '48', '72', '96', '120', '144', '168']
const CATERING_AMOUNT_OPTIONS = ['200', '500', '1000', '2000', '3000', '4000', '5000', '10000', '15000', '20000', '50000', '100000', '200000']
const AIRCRAFT_WEAR_PERCENTS = ['10', '20', '30', '40', '50', '60', '70', '80', '90']

function num(v: string): number | undefined {
  if (v === '') return undefined
  const n = Number(v)
  return Number.isNaN(n) ? undefined : n
}

/**
 * Everything internal/config.Config supports beyond what the rest of the
 * node form already covers (login, services, schedule, timezone) -- the
 * "maintenance" and "purchasing" parameters that used to only exist in a
 * standalone config.yaml (see the main README's Configuration table).
 * Stored in nodes.extra_config, merged on top of config.Config's own
 * defaults when ambot fetches its config -- see
 * internal/api/internal_handlers.go's doc comment.
 *
 * Every field is disabled unless a service that actually reads it is
 * currently selected -- there's no point letting someone tune
 * aircraft_wear_percent on a node that doesn't run ac_maintenance, and a
 * value set while a service was off would otherwise just sit there
 * silently unused if the service gets turned back on later without the
 * user remembering to revisit it.
 *
 * log_level is deliberately NOT here -- see AdminNodeDetailPage's
 * diagnostics section, an admin-only control on a different page.
 *
 * Lives inside NodeFormPage's Services card, right below the service
 * list, collapsed by default -- per the user's own explicit call: a
 * separate card at the bottom of the page buried the connection between
 * "which services are on" and "which of these groups actually apply".
 * Since it stays collapsed even as that connection changes, the count
 * badge plus a brief highlight flash on the toggle row (triggered
 * whenever the count itself changes, not on every keystroke) is the
 * signal that something in here just became relevant -- deliberately NOT
 * auto-expanding, which would be worse: it would yank the page away from
 * whatever the user was just doing in the service list above it.
 */
export function AdvancedSettingsSection({
  services,
  value,
  onChange,
}: {
  services: string[]
  value: NodeExtraConfig
  onChange: (v: NodeExtraConfig) => void
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const [flash, setFlash] = useState(false)

  const hasFuel = services.includes('buy_fuel')
  const hasMarketing = services.includes('marketing')
  const hasMaintenance = services.includes('ac_maintenance')
  const hasHubs = services.includes('hubs')
  const hasAlliance = services.includes('alliance_stats')
  // budget_percent.maintenance funds BOTH ac_maintenance's own work AND
  // hubs' lounge-repair/catering spending -- see internal/bot/hub.go and
  // internal/bot/maintenance.go, both draw from the same budget bucket.
  const hasMaintenanceBudget = hasMaintenance || hasHubs
  const cateringSubfieldsEnabled = hasHubs && (value.buy_catering_if_missing ?? true)

  const activeCount = [hasFuel, hasMarketing, hasMaintenance, hasHubs, hasAlliance].filter(Boolean).length

  // Flash the toggle row whenever the ACTIVE COUNT changes (a service with
  // parameters got turned on/off), not on every render -- a ref holds the
  // previous count across renders without itself triggering one.
  const prevActiveCount = useRef(activeCount)

  useEffect(() => {
    if (prevActiveCount.current === activeCount) {
      return
    }

    prevActiveCount.current = activeCount
    setFlash(true)
    const timer = setTimeout(() => setFlash(false), 900)

    return () => clearTimeout(timer)
  }, [activeCount])

  function set<K extends keyof NodeExtraConfig>(key: K, v: NodeExtraConfig[K]) {
    onChange({ ...value, [key]: v })
  }

  // Alliance IDs needs its own local, freely-typable text buffer --
  // deriving the input's value directly from the committed (already
  // digits-only-filtered) array, as every other field here does, breaks
  // mid-typing: a trailing comma before the next number, or a stray
  // letter, would otherwise vanish immediately because the half-typed
  // text never round-trips through the filter as a valid array. See the
  // input's own onChange below for the character-level (not per-segment)
  // sanitization that fixes this.
  const [allianceIdsText, setAllianceIdsText] = useState(() => (value.alliance_ids ?? []).join(', '))
  // Shown briefly (matching the flash pattern above) when a disallowed
  // character is actually rejected, so the user gets the same "why
  // didn't that appear" feedback a normal registration-form field gives
  // -- rather than the character just silently not showing up.
  const [allianceIdsInvalidHint, setAllianceIdsInvalidHint] = useState(false)
  const allianceIdsHintTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => () => clearTimeout(allianceIdsHintTimer.current), [])

  function flashAllianceIdsHint() {
    setAllianceIdsInvalidHint(true)
    clearTimeout(allianceIdsHintTimer.current)
    allianceIdsHintTimer.current = setTimeout(() => setAllianceIdsInvalidHint(false), 2000)
  }

  // Resync from value ONLY when it changed for a reason other than our
  // own last onChange echoing back (e.g. switching to a different node) --
  // comparing the committed array against what allianceIdsText itself
  // would currently parse to tells the two cases apart.
  useEffect(() => {
    const parsedFromText = allianceIdsText
      .split(',')
      .map((s) => s.trim())
      .filter((s) => /^\d+$/.test(s))
    const incoming = value.alliance_ids ?? []

    if (JSON.stringify(parsedFromText) !== JSON.stringify(incoming)) {
      setAllianceIdsText(incoming.join(', '))
    }
    // Deliberately only watching value.alliance_ids, not allianceIdsText
    // itself -- this effect exists to catch EXTERNAL changes, not to run
    // on every local keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value.alliance_ids])

  function setBudget(key: 'fuel' | 'maintenance' | 'marketing', v: number | undefined) {
    onChange({ ...value, budget_percent: { ...value.budget_percent, [key]: v } })
  }

  function setGoodPrice(key: 'fuel' | 'co2', v: number | undefined) {
    onChange({ ...value, good_price: { ...value.good_price, [key]: v } })
  }

  return (
    <div className="flex flex-col gap-3 border-t pt-4">
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className={cn(
          'flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left transition-colors duration-700',
          flash ? 'bg-primary/15' : 'bg-transparent',
        )}
      >
        <span className="flex items-center gap-2 text-sm font-medium">
          {t('nodes.form.sectionAdvanced')}
          <Badge variant={activeCount > 0 ? 'success' : 'secondary'}>{t('nodes.advanced.activeCount', { count: activeCount })}</Badge>
        </span>
        <ChevronDownIcon className={cn('size-4 shrink-0 text-muted-foreground transition-transform', expanded && 'rotate-180')} />
      </button>
      {expanded && (
        <div className="flex flex-col gap-6 pt-1">
          <p className="text-xs text-muted-foreground">{t('nodes.advanced.intro')}</p>

          {/* buy_fuel */}
          <fieldset disabled={!hasFuel} className="flex flex-col gap-3 disabled:opacity-40">
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-medium">{t('nodes.advanced.fuelGroup')}</span>
              <InfoHint text={t('nodes.advanced.fuelGroupHint')} />
            </div>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="good-price-fuel" className="flex items-center gap-1">
                  {t('nodes.advanced.goodPriceFuel')}
                  <InfoHint text={t('nodes.advanced.goodPriceFuelHint')} />
                </Label>
                <Input
                  id="good-price-fuel"
                  type="number"
                  min={0}
                  placeholder="500"
                  value={value.good_price?.fuel ?? ''}
                  onChange={(e) => setGoodPrice('fuel', num(e.target.value))}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="good-price-co2" className="flex items-center gap-1">
                  {t('nodes.advanced.goodPriceCo2')}
                  <InfoHint text={t('nodes.advanced.goodPriceCo2Hint')} />
                </Label>
                <Input
                  id="good-price-co2"
                  type="number"
                  min={0}
                  placeholder="120"
                  value={value.good_price?.co2 ?? ''}
                  onChange={(e) => setGoodPrice('co2', num(e.target.value))}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="fuel-critical-percent" className="flex items-center gap-1">
                  {t('nodes.advanced.fuelCriticalPercent')}
                  <InfoHint text={t('nodes.advanced.fuelCriticalPercentHint')} />
                </Label>
                <Input
                  id="fuel-critical-percent"
                  type="number"
                  min={0}
                  max={100}
                  placeholder="20"
                  value={value.fuel_critical_percent ?? ''}
                  onChange={(e) => set('fuel_critical_percent', num(e.target.value))}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="budget-fuel" className="flex items-center gap-1">
                  {t('nodes.advanced.budgetFuel')}
                  <InfoHint text={t('nodes.advanced.budgetFuelHint')} />
                </Label>
                <Input
                  id="budget-fuel"
                  type="number"
                  min={0}
                  max={100}
                  placeholder="70"
                  value={value.budget_percent?.fuel ?? ''}
                  onChange={(e) => setBudget('fuel', num(e.target.value))}
                />
              </div>
            </div>
          </fieldset>

          {/* marketing */}
          <fieldset disabled={!hasMarketing} className="flex flex-col gap-3 disabled:opacity-40">
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-medium">{t('nodes.advanced.marketingGroup')}</span>
              <InfoHint text={t('nodes.advanced.marketingGroupHint')} />
            </div>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="budget-marketing" className="flex items-center gap-1">
                  {t('nodes.advanced.budgetMarketing')}
                  <InfoHint text={t('nodes.advanced.budgetMarketingHint')} />
                </Label>
                <Input
                  id="budget-marketing"
                  type="number"
                  min={0}
                  max={100}
                  placeholder="70"
                  value={value.budget_percent?.marketing ?? ''}
                  onChange={(e) => setBudget('marketing', num(e.target.value))}
                />
              </div>
            </div>
          </fieldset>

          {/* ac_maintenance (+ shared maintenance budget with hubs) */}
          <fieldset disabled={!hasMaintenance} className="flex flex-col gap-3 disabled:opacity-40">
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-medium">{t('nodes.advanced.maintenanceGroup')}</span>
              <InfoHint text={t('nodes.advanced.maintenanceGroupHint')} />
            </div>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="aircraft-wear-percent" className="flex items-center gap-1">
                  {t('nodes.advanced.aircraftWearPercent')}
                  <InfoHint text={t('nodes.advanced.aircraftWearPercentHint')} />
                </Label>
                <Select
                  value={value.aircraft_wear_percent ?? ''}
                  onValueChange={(v) => set('aircraft_wear_percent', v)}
                  disabled={!hasMaintenance}
                >
                  <SelectTrigger id="aircraft-wear-percent">
                    <SelectValue placeholder="80" />
                  </SelectTrigger>
                  <SelectContent>
                    {AIRCRAFT_WEAR_PERCENTS.map((v) => (
                      <SelectItem key={v} value={v}>
                        {v}%
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="aircraft-max-hours" className="flex items-center gap-1">
                  {t('nodes.advanced.aircraftMaxHoursToCheck')}
                  <InfoHint text={t('nodes.advanced.aircraftMaxHoursToCheckHint')} />
                </Label>
                <Input
                  id="aircraft-max-hours"
                  type="number"
                  min={0}
                  placeholder="24"
                  value={value.aircraft_max_hours_to_check ?? ''}
                  onChange={(e) => set('aircraft_max_hours_to_check', num(e.target.value))}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="aircraft-modify-limit" className="flex items-center gap-1">
                  {t('nodes.advanced.aircraftModifyLimit')}
                  <InfoHint text={t('nodes.advanced.aircraftModifyLimitHint')} />
                </Label>
                <Input
                  id="aircraft-modify-limit"
                  type="number"
                  min={0}
                  placeholder="3"
                  value={value.aircraft_modify_limit ?? ''}
                  onChange={(e) => set('aircraft_modify_limit', num(e.target.value))}
                />
              </div>
            </div>
          </fieldset>

          {/* hubs */}
          <fieldset disabled={!hasHubs} className="flex flex-col gap-3 disabled:opacity-40">
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-medium">{t('nodes.advanced.hubsGroup')}</span>
              <InfoHint text={t('nodes.advanced.hubsGroupHint')} />
            </div>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="hubs-maintenance-limit" className="flex items-center gap-1">
                  {t('nodes.advanced.hubsMaintenanceLimit')}
                  <InfoHint text={t('nodes.advanced.hubsMaintenanceLimitHint')} />
                </Label>
                <Input
                  id="hubs-maintenance-limit"
                  type="number"
                  min={0}
                  placeholder="5"
                  value={value.hubs_maintenance_limit ?? ''}
                  onChange={(e) => set('hubs_maintenance_limit', num(e.target.value))}
                />
              </div>
              <div className="flex flex-col gap-1.5 sm:col-span-2">
                <Label className="flex items-center gap-1">
                  {t('nodes.advanced.budgetMaintenance')}
                  <InfoHint text={t('nodes.advanced.budgetMaintenanceHint')} />
                </Label>
                <Input
                  type="number"
                  min={0}
                  max={100}
                  placeholder="30"
                  disabled={!hasMaintenanceBudget}
                  value={value.budget_percent?.maintenance ?? ''}
                  onChange={(e) => setBudget('maintenance', num(e.target.value))}
                />
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-6">
              <div className="flex items-center gap-2">
                <Switch
                  id="repair-lounges"
                  checked={value.repair_lounges ?? true}
                  onCheckedChange={(v) => set('repair_lounges', v)}
                />
                <Label htmlFor="repair-lounges" className="flex items-center gap-1">
                  {t('nodes.advanced.repairLounges')}
                  <InfoHint text={t('nodes.advanced.repairLoungesHint')} />
                </Label>
              </div>
              <div className="flex items-center gap-2">
                <Switch
                  id="buy-catering"
                  checked={value.buy_catering_if_missing ?? true}
                  onCheckedChange={(v) => set('buy_catering_if_missing', v)}
                />
                <Label htmlFor="buy-catering" className="flex items-center gap-1">
                  {t('nodes.advanced.buyCateringIfMissing')}
                  <InfoHint text={t('nodes.advanced.buyCateringIfMissingHint')} />
                </Label>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="catering-duration" className="flex items-center gap-1">
                  {t('nodes.advanced.cateringDurationHours')}
                  <InfoHint text={t('nodes.advanced.cateringDurationHoursHint')} />
                </Label>
                <Select
                  value={value.catering_duration_hours ?? ''}
                  onValueChange={(v) => set('catering_duration_hours', v)}
                  disabled={!cateringSubfieldsEnabled}
                >
                  <SelectTrigger id="catering-duration">
                    <SelectValue placeholder="168" />
                  </SelectTrigger>
                  <SelectContent>
                    {CATERING_DURATION_HOURS.map((v) => (
                      <SelectItem key={v} value={v}>
                        {v}h
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="catering-amount" className="flex items-center gap-1">
                  {t('nodes.advanced.cateringAmountOption')}
                  <InfoHint text={t('nodes.advanced.cateringAmountOptionHint')} />
                </Label>
                <Select
                  value={value.catering_amount_option ?? ''}
                  onValueChange={(v) => set('catering_amount_option', v)}
                  disabled={!cateringSubfieldsEnabled}
                >
                  <SelectTrigger id="catering-amount">
                    <SelectValue placeholder="20000" />
                  </SelectTrigger>
                  <SelectContent>
                    {CATERING_AMOUNT_OPTIONS.map((v) => (
                      <SelectItem key={v} value={v}>
                        {v}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          </fieldset>

          {/* alliance_stats */}
          <fieldset disabled={!hasAlliance} className="flex flex-col gap-3 disabled:opacity-40">
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-medium">{t('nodes.advanced.allianceGroup')}</span>
              <InfoHint text={t('nodes.advanced.allianceGroupHint')} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="alliance-ids" className="flex items-center gap-1">
                {t('nodes.advanced.allianceIds')}
                <InfoHint text={t('nodes.advanced.allianceIdsHint')} />
              </Label>
              <Input
                id="alliance-ids"
                placeholder="12345, 67890"
                inputMode="numeric"
                aria-invalid={allianceIdsInvalidHint}
                aria-describedby="alliance-ids-hint"
                value={allianceIdsText}
                onKeyDown={(e) => {
                  // Block a disallowed character BEFORE it's typed
                  // (rather than filtering it out after the fact in
                  // onChange) -- this is the standard "restricted input"
                  // pattern: the character never appears at all, so
                  // there's no visible flash-then-vanish and no cursor
                  // jump to work around. Modifier-held keys (Ctrl+V
                  // paste, Ctrl+A select-all, ...) and non-printing keys
                  // (Backspace, arrows, ...) are deliberately left alone
                  // -- e.key.length === 1 is only true for an actual
                  // printable character.
                  if (e.ctrlKey || e.metaKey || e.altKey || e.key.length !== 1) {
                    return
                  }

                  if (!/[\d,\s]/.test(e.key)) {
                    e.preventDefault()
                    flashAllianceIdsHint()
                  }
                }}
                onChange={(e) => {
                  // Safety net for input onKeyDown can't see -- paste,
                  // drag-drop, IME composition. Same character-level
                  // filtering (not per-segment): strip anything that
                  // isn't a digit, comma, or space, but otherwise keep
                  // the raw text as-is -- a trailing comma/space while
                  // composing the next ID is legitimate mid-typing
                  // state, not yet invalid.
                  const sanitized = e.target.value.replace(/[^\d,\s]/g, '')
                  if (sanitized !== e.target.value) {
                    flashAllianceIdsHint()
                  }
                  setAllianceIdsText(sanitized)
                  set(
                    'alliance_ids',
                    sanitized
                      .split(',')
                      .map((s) => s.trim())
                      // Alliance IDs are purely numeric (they're used
                      // as-is in a game URL, see
                      // internal/bot/stats.go's allianceStatsByID) --
                      // only complete, fully-numeric segments make it
                      // into the committed array; an empty segment from
                      // a trailing/leading comma is simply not committed
                      // yet, not an error.
                      .filter((s) => /^\d+$/.test(s)),
                  )
                }}
              />
              {allianceIdsInvalidHint && (
                <p id="alliance-ids-hint" role="alert" className="text-xs text-destructive">
                  {t('nodes.advanced.allianceIdsInvalidChar')}
                </p>
              )}
            </div>
          </fieldset>
        </div>
      )}
    </div>
  )
}
