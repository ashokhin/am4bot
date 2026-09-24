import { useTranslation } from 'react-i18next'
import { DELTA_PERIODS, type DeltaPeriod } from '../api/metrics'
import type { MetricSeries } from '../api/types'
import { Card, CardContent, CardHeader, CardTitle } from './ui/card'
import { Tabs, TabsList, TabsTrigger } from './ui/tabs'

// "How much changed over this window" companion to NodeMetricTiles' raw
// all-time gauges. Backed by GET /api/metrics/delta?period=... (see
// internal/api/metrics_handlers.go's deltaMetrics/deltaPeriods), a single
// instant query per metric, no query_range/charting involved yet.
//
// am4_flights_departed_total, not am4_stats_flights_operated_total --
// see deltaMetrics' own doc comment in metrics_handlers.go for why: the
// latter is the whole airline account's lifetime flights, identical
// across every node on the same account regardless of which services
// that node runs, so it leaked a nonzero "flights dispatched" delta onto
// nodes that never run depart at all.
const DELTA_TILE_METRICS: { metric: string; labelKey: string; format: (v: number) => string }[] = [
  { metric: 'am4_flights_departed_total', labelKey: 'metrics.delta.flightsOperated', format: (v) => Math.round(v).toLocaleString() },
]

interface NodeForDeltaTiles {
  id: number
  name: string
}

export function NodeMetricDeltaTiles({
  nodes,
  series,
  period,
  onPeriodChange,
}: {
  nodes: NodeForDeltaTiles[]
  series: MetricSeries[]
  period: DeltaPeriod
  onPeriodChange: (period: DeltaPeriod) => void
}) {
  const { t } = useTranslation()

  function valueFor(nodeId: number, metric: string): number | undefined {
    return series.find((s) => s.node_id === nodeId && s.metric === metric)?.value
  }

  if (nodes.length === 0) {
    return null
  }

  return (
    <Card className="node-delta-card">
      <CardHeader className="flex flex-row items-center justify-between gap-2">
        <CardTitle className="text-base">{t('metrics.delta.title')}</CardTitle>
        <Tabs value={period} onValueChange={(v) => onPeriodChange(v as DeltaPeriod)}>
          <TabsList>
            {DELTA_PERIODS.map((p) => (
              <TabsTrigger key={p} value={p} data-testid={`delta-period-${p}`}>
                {t(`metrics.delta.period.${p}`)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
          {nodes.map((node) =>
            DELTA_TILE_METRICS.map(({ metric, labelKey, format }) => {
              const value = valueFor(node.id, metric)

              return (
                <div key={`${node.id}-${metric}`}>
                  <div className="text-xs text-muted-foreground">
                    {node.name} — {t(labelKey)}
                  </div>
                  <div className="text-xl font-semibold">{value === undefined ? '—' : `+${format(value)}`}</div>
                </div>
              )
            }),
          )}
        </div>
      </CardContent>
    </Card>
  )
}
