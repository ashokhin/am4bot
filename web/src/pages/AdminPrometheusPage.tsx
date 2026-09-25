import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { metricsApi } from '../api/metrics'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'

/** Admin-only: configures the single Prometheus instance apiserver's /api/metrics proxies queries to. */
export function AdminPrometheusPage() {
  const { t } = useTranslation()
  const [url, setUrl] = useState('')
  const [configured, setConfigured] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [error, setError] = useState<string | undefined>(undefined)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    metricsApi
      .getPrometheusSettings()
      .then((s) => {
        setConfigured(s.configured)
        setUrl(s.url ?? '')
      })
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }, [])

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(undefined)
    setSubmitting(true)

    try {
      const s = await metricsApi.setPrometheusSettings({ url })
      setConfigured(s.configured)
      toast.success(t('metricsAdmin.saved'))
    } catch {
      setError(t('metricsAdmin.errors.saveFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return <p className="text-muted-foreground">{t('common.loading')}</p>
  }
  if (loadError) {
    return (
      <p role="alert" className="text-destructive">
        {t('metricsAdmin.errors.loadFailed')}
      </p>
    )
  }

  return (
    <div className="flex max-w-6xl flex-col gap-4">
      <h1 className="text-2xl font-semibold">{t('metricsAdmin.title')}</h1>
      <Card>
        <CardHeader className="flex flex-col items-start gap-2 sm:flex-row sm:items-center sm:justify-between">
          <CardTitle className="text-base">{t('metricsAdmin.title')}</CardTitle>
          <Badge variant={configured ? 'success' : 'secondary'} className="whitespace-normal text-left">
            {configured ? t('metricsAdmin.configured') : t('metricsAdmin.notConfigured')}
          </Badge>
        </CardHeader>
        <CardContent>
          <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="prometheus-url">{t('metricsAdmin.form.url')}</Label>
              <Input
                id="prometheus-url"
                type="url"
                name="prometheus_url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="http://prometheus:9090"
                required
                autoFocus
              />
            </div>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <Button type="submit" disabled={submitting} className="w-fit">
              {t('common.save')}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
