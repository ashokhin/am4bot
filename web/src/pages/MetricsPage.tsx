import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { type DeltaPeriod, metricsApi } from '../api/metrics'
import { nodesApi } from '../api/nodes'
import type { MetricSeries, Node } from '../api/types'
import { LastUpdated } from '../components/LastUpdated'
import { NodeBalanceChart } from '../components/NodeBalanceChart'
import { NodeMetricDeltaTiles } from '../components/NodeMetricDeltaTiles'
import { NodeMetricTiles } from '../components/NodeMetricTiles'
import { usePolling } from '../hooks/usePolling'

export function MetricsPage() {
  const { t } = useTranslation()
  const [nodes, setNodes] = useState<Node[]>([])
  const [series, setSeries] = useState<MetricSeries[]>([])
  const [deltaSeries, setDeltaSeries] = useState<MetricSeries[]>([])
  const [deltaPeriod, setDeltaPeriod] = useState<DeltaPeriod>('24h')
  const [balanceSeries, setBalanceSeries] = useState<MetricSeries[]>([])
  const [balancePeriod, setBalancePeriod] = useState<DeltaPeriod>('7d')
  const [configured, setConfigured] = useState(true)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [lastUpdatedAt, setLastUpdatedAt] = useState<Date | null>(null)

  usePolling(() => {
    Promise.all([nodesApi.list(), metricsApi.get(), metricsApi.getDelta(deltaPeriod), metricsApi.getBalance(balancePeriod)])
      .then(([n, m, d, b]) => {
        setNodes(n)
        setConfigured(m.configured)
        setSeries(m.series ?? [])
        setDeltaSeries(d.series ?? [])
        setBalanceSeries(b.series ?? [])
        setLastUpdatedAt(new Date())
        setLoadError(false)
      })
      .catch(() => {
        // Only the FIRST load failing is fatal (shows the full-page
        // error state below) -- a transient hiccup on a later poll just
        // leaves the previously-good data on screen instead of blanking
        // the whole page out.
        setLoadError((prev) => prev || lastUpdatedAt === null)
      })
      .finally(() => setLoading(false))
  }, [deltaPeriod, balancePeriod])

  if (loading) {
    return <p className="text-muted-foreground">{t('common.loading')}</p>
  }
  if (loadError) {
    return (
      <p role="alert" className="text-destructive">
        {t('metrics.errors.loadFailed')}
      </p>
    )
  }
  if (!configured) {
    return <p className="text-muted-foreground">{t('metrics.notConfigured')}</p>
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-baseline justify-between gap-2">
        <h1 className="text-2xl font-semibold">{t('metrics.title')}</h1>
        <LastUpdated at={lastUpdatedAt} />
      </div>
      <NodeMetricDeltaTiles nodes={nodes} series={deltaSeries} period={deltaPeriod} onPeriodChange={setDeltaPeriod} />
      <NodeBalanceChart nodes={nodes} series={balanceSeries} period={balancePeriod} onPeriodChange={setBalancePeriod} />
      <NodeMetricTiles nodes={nodes} series={series} />
    </div>
  )
}
