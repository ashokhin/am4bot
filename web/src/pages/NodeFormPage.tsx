import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useBlocker, useNavigate, useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { ApiError } from '../api/client'
import { nodesApi } from '../api/nodes'
import type { NodeExtraConfig } from '../api/types'
import { AdvancedSettingsSection } from '../components/AdvancedSettingsSection'
import { BackButton } from '../components/BackButton'
import { CronScheduleEditor } from '../components/CronScheduleEditor'
import { ServiceListEditor } from '../components/ServiceListEditor'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '../components/ui/dialog'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { PasswordInput } from '../components/ui/password-input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select'
import { Switch } from '../components/ui/switch'
import { getTimezoneOptions } from '../lib/timezones'

/** Handles both /nodes/new (id undefined) and /nodes/:id (edit). */
export function NodeFormPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { id } = useParams()
  const isEdit = id !== undefined

  const [name, setName] = useState('')
  const [gameUsername, setGameUsername] = useState('')
  const [gamePassword, setGamePassword] = useState('')
  const [services, setServices] = useState<string[]>([])
  const [schedules, setSchedules] = useState<string[]>(['*/5 * * * *'])
  const [jitterSeconds, setJitterSeconds] = useState(0)
  const [timeoutSeconds, setTimeoutSeconds] = useState(180)
  const [timezone, setTimezone] = useState('UTC')
  // Whether the node being edited already has a real game password to
  // fall back to if the field below is left blank - a freshly
  // auto-created default node never does, even though it's reached via
  // the edit route (isEdit alone isn't enough to tell). See
  // nodeResponse.HasGamePassword's doc comment on the Go side.
  const [hasGamePassword, setHasGamePassword] = useState(false)
  const [enabled, setEnabled] = useState(false)
  const [extraConfig, setExtraConfig] = useState<NodeExtraConfig>({})
  const [loading, setLoading] = useState(isEdit)
  const timezoneOptions = useMemo(() => getTimezoneOptions(), [])
  const [error, setError] = useState<string | undefined>(undefined)
  const [submitting, setSubmitting] = useState(false)

  // Everything the user can edit, serialized: dirty is "differs from what was
  // loaded". Null baseline means "not loaded yet" - nothing is dirty then.
  const snapshot = JSON.stringify({
    name,
    gameUsername,
    gamePassword,
    services,
    schedules,
    jitterSeconds,
    timeoutSeconds,
    timezone,
    enabled,
    extraConfig,
  })
  const [baseline, setBaseline] = useState<string | null>(isEdit ? null : snapshot)
  const dirty = baseline !== null && snapshot !== baseline
  // Set just before a successful save navigates away, so the blocker below
  // doesn't stop that navigation.
  const allowLeaveRef = useRef(false)

  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirty && !allowLeaveRef.current && currentLocation.pathname !== nextLocation.pathname,
  )

  useEffect(() => {
    if (!dirty) {
      return
    }

    function onBeforeUnload(e: BeforeUnloadEvent) {
      e.preventDefault()
      e.returnValue = ''
    }

    window.addEventListener('beforeunload', onBeforeUnload)

    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  useEffect(() => {
    if (!isEdit) {
      return
    }

    nodesApi
      .get(Number(id))
      .then((node) => {
        setBaseline(
          JSON.stringify({
            name: node.name,
            gameUsername: node.game_username,
            gamePassword: '',
            services: node.services,
            schedules: node.cron_schedules,
            jitterSeconds: node.cron_jitter_seconds,
            timeoutSeconds: node.timeout_seconds,
            timezone: node.timezone,
            enabled: node.enabled,
            extraConfig: node.extra_config,
          }),
        )
        setName(node.name)
        setGameUsername(node.game_username)
        setServices(node.services)
        setSchedules(node.cron_schedules)
        setJitterSeconds(node.cron_jitter_seconds)
        setTimeoutSeconds(node.timeout_seconds)
        setTimezone(node.timezone)
        setHasGamePassword(node.has_game_password)
        setEnabled(node.enabled)
        // A plain type assertion, not a runtime copy - any key this
        // form doesn't know about (e.g. "log_level", which only an
        // admin ever sets, see AdminNodeDetailPage) rides along
        // untouched through every spread AdvancedSettingsSection does,
        // and gets sent back as-is on save instead of silently dropped.
        setExtraConfig(node.extra_config as NodeExtraConfig)
      })
      .catch(() => setError(t('nodes.errors.loadFailed')))
      .finally(() => setLoading(false))
  }, [id, isEdit, t])

  function stay() {
    if (blocker.state === 'blocked') {
      blocker.reset()
    }
  }

  function leave() {
    if (blocker.state === 'blocked') {
      blocker.proceed()
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(undefined)
    setSubmitting(true)

    try {
      if (isEdit) {
        await nodesApi.update(Number(id), {
          name,
          game_username: gameUsername,
          game_password: gamePassword || undefined,
          services,
          cron_schedules: schedules,
          cron_jitter_seconds: jitterSeconds,
          timeout_seconds: timeoutSeconds,
          timezone,
          extra_config: extraConfig,
          enabled,
        })
        toast.success(t('nodes.updated', { name }))
      } else {
        await nodesApi.create({
          name,
          game_username: gameUsername,
          game_password: gamePassword,
          services,
          cron_schedules: schedules,
          cron_jitter_seconds: jitterSeconds,
          timeout_seconds: timeoutSeconds,
          timezone,
          extra_config: extraConfig,
        })
        toast.success(t('nodes.created', { name }))
      }

      allowLeaveRef.current = true
      navigate('/nodes')
    } catch (err) {
      if (err instanceof ApiError && err.status === 400) {
        setError(err.message)
      } else {
        setError(isEdit ? t('nodes.errors.updateFailed') : t('nodes.errors.createFailed'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return <p className="text-muted-foreground">{t('common.loading')}</p>
  }

  return (
    <div className="flex max-w-6xl flex-col gap-4">
      <BackButton to="/nodes" />
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{isEdit ? t('nodes.form.editTitle') : t('nodes.form.createTitle')}</h1>
        {isEdit && (
          <div className="flex items-center gap-2">
            <Switch id="node-enabled" checked={enabled} onCheckedChange={setEnabled} />
            <Label htmlFor="node-enabled" className="text-sm">
              {enabled ? t('nodes.status.enabled') : t('nodes.status.disabled')}
            </Label>
          </div>
        )}
      </div>
      {isEdit && (
        <p className="text-xs text-muted-foreground">
          {t('nodes.form.nodeId')}: <span className="select-all font-mono">{id}</span>
        </p>
      )}
      {isEdit && <p className="text-xs text-muted-foreground">{t('nodes.form.enabledHint')}</p>}
      <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t('nodes.form.sectionAccount')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="name">{t('nodes.form.name')}</Label>
              <Input id="name" name="name" value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="game_username">{t('nodes.form.gameUsername')}</Label>
              <Input
                id="game_username"
                name="game_username"
                value={gameUsername}
                onChange={(e) => setGameUsername(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="game_password">{t('nodes.form.gamePassword')}</Label>
              <PasswordInput
                id="game_password"
                name="game_password"
                value={gamePassword}
                onChange={(e) => setGamePassword(e.target.value)}
                required={!hasGamePassword}
                placeholder={hasGamePassword ? t('nodes.form.gamePasswordEditPlaceholder') : ''}
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t('nodes.form.sectionServices')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <ServiceListEditor value={services} onChange={setServices} />
            <AdvancedSettingsSection services={services} value={extraConfig} onChange={setExtraConfig} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t('nodes.form.sectionSchedule')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <CronScheduleEditor value={schedules} onChange={setSchedules} />
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="timezone">{t('common.timezone')}</Label>
              <Select value={timezone} onValueChange={setTimezone}>
                <SelectTrigger id="timezone" className="w-full max-w-80">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {timezoneOptions.map((zone) => (
                    <SelectItem key={zone.value} value={zone.value}>
                      {zone.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{t('nodes.form.timezoneHint')}</p>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="jitter">{t('nodes.form.jitterSeconds')}</Label>
              <Input
                id="jitter"
                type="number"
                min={0}
                className="max-w-40"
                value={jitterSeconds}
                onChange={(e) => setJitterSeconds(Number(e.target.value))}
              />
              <p className="text-xs text-muted-foreground">{t('nodes.form.jitterHint')}</p>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="timeout">{t('nodes.form.timeoutSeconds')}</Label>
              <Input
                id="timeout"
                type="number"
                min={1}
                className="max-w-40"
                value={timeoutSeconds}
                onChange={(e) => setTimeoutSeconds(Number(e.target.value))}
              />
            </div>
          </CardContent>
        </Card>

        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}

        <div className="flex gap-2">
          <Button type="submit" disabled={submitting}>
            {t('common.save')}
          </Button>
          <Button type="button" variant="outline" onClick={() => navigate('/nodes')}>
            {t('common.cancel')}
          </Button>
        </div>
      </form>
      <Dialog open={blocker.state === 'blocked'} onOpenChange={(open) => !open && stay()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('nodes.form.unsavedTitle')}</DialogTitle>
            <DialogDescription>{t('nodes.form.unsavedDescription')}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={stay}>
              {t('nodes.form.unsavedStay')}
            </Button>
            <Button variant="destructive" onClick={leave}>
              {t('nodes.form.unsavedLeave')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
