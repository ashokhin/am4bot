import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { adminApi } from '../api/admin'
import type { AdminNodeView, SetNodeLogLevelRequest } from '../api/types'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Label } from '../components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select'

const NO_OVERRIDE = '__default__'

/**
 * Admin-only, read-only: the same information a node's own owner sees on
 * NodeFormPage, minus game login/password (shows "owner: <login>"
 * instead) -- an admin can look, never touch, with exactly one
 * exception: the Diagnostics card below lets them set this node's
 * log_level, for debugging a misbehaving node without needing any other
 * access to it (a regular user never sees this field at all -- see
 * NodeFormPage/AdvancedSettingsSection, which deliberately don't render
 * it). Reachable from AdminNodesPage and AdminUserDetailPage.
 */
export function AdminNodeDetailPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { id } = useParams()
  const [node, setNode] = useState<AdminNodeView | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [savingLogLevel, setSavingLogLevel] = useState(false)

  function load() {
    adminApi
      .getNode(Number(id))
      .then(setNode)
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }

  useEffect(load, [id])

  async function handleSetLogLevel(value: string) {
    const logLevel = (value === NO_OVERRIDE ? '' : value) as SetNodeLogLevelRequest['log_level']
    setSavingLogLevel(true)

    try {
      await adminApi.setNodeLogLevel(Number(id), { log_level: logLevel })
      load()
      toast.success(t('adminNodes.diagnostics.saved'))
    } catch {
      toast.error(t('adminNodes.diagnostics.errors.saveFailed'))
    } finally {
      setSavingLogLevel(false)
    }
  }

  if (loading) {
    return <p className="text-muted-foreground">{t('common.loading')}</p>
  }
  if (loadError || !node) {
    return (
      <p role="alert" className="text-destructive">
        {t('adminNodes.errors.loadFailed')}
      </p>
    )
  }

  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          {node.name}
          {node.is_default && <Badge variant="secondary">{t('nodes.defaultBadge')}</Badge>}
        </h1>
        <Badge variant={node.enabled ? 'success' : 'secondary'}>
          {node.enabled ? t('nodes.status.enabled') : t('nodes.status.disabled')}
        </Badge>
      </div>

      <Card>
        <CardContent className="flex flex-col gap-3 pt-6">
          <div>
            <Label>{t('adminNodes.table.owner')}</Label>
            <p className="text-sm">{node.owner_login}</p>
          </div>
          <div>
            <Label>{t('common.timezone')}</Label>
            <p className="text-sm">{node.timezone}</p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t('nodes.form.sectionServices')}</CardTitle>
        </CardHeader>
        <CardContent>
          {node.services.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t('nodes.services.none')}</p>
          ) : (
            <ol className="flex flex-col gap-1.5">
              {node.services.map((service, i) => (
                // eslint-disable-next-line react/no-array-index-key
                <li key={i} className="rounded-md border bg-card px-3 py-2 text-sm">
                  {i + 1}. {service}
                </li>
              ))}
            </ol>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t('nodes.form.sectionSchedule')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            {node.cron_schedules.map((cron, i) => (
              // eslint-disable-next-line react/no-array-index-key
              <p key={i} className="font-mono text-sm">
                {cron}
              </p>
            ))}
          </div>
          <div>
            <Label>{t('nodes.form.jitterSeconds')}</Label>
            <p className="text-sm">{node.cron_jitter_seconds}</p>
          </div>
          <div>
            <Label>{t('nodes.form.timeoutSeconds')}</Label>
            <p className="text-sm">{node.timeout_seconds}</p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t('adminNodes.diagnostics.title')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          <Label htmlFor="log-level">{t('adminNodes.diagnostics.logLevel')}</Label>
          <Select
            value={(node.extra_config.log_level as string | undefined) ?? NO_OVERRIDE}
            disabled={savingLogLevel}
            onValueChange={(v) => void handleSetLogLevel(v)}
          >
            <SelectTrigger id="log-level" className="w-full max-w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NO_OVERRIDE}>{t('adminNodes.diagnostics.default')}</SelectItem>
              <SelectItem value="debug">debug</SelectItem>
              <SelectItem value="info">info</SelectItem>
              <SelectItem value="warn">warn</SelectItem>
              <SelectItem value="error">error</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t('adminNodes.diagnostics.hint')}</p>
        </CardContent>
      </Card>

      <Button variant="outline" onClick={() => navigate(-1)} className="w-fit">
        {t('common.back')}
      </Button>
    </div>
  )
}
