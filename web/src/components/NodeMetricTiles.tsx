import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { MetricSeries } from '../api/types'
import { Card, CardContent, CardHeader, CardTitle } from './ui/card'
import { cn } from '../lib/utils'

// Stat-tile dashboard: one card per node, a handful of named gauges as
// tiles. No time-series chart in this first version -- see
// internal/api/metrics_handlers.go's exposedMetrics for why only scalar
// gauges are wired up so far. Palette/spacing follow the dataviz skill's
// stat-tile contract (label, value, status color for up/down). Shared
// between MetricsPage (a user's own nodes) and AdminMetricsPage (any
// user's nodes, picked by an admin) -- the rendering is identical, only
// which series/nodes get passed in differs.

// Renders a Unix-timestamp value as two explicit lines (date, then time)
// rather than one long toLocaleString() -- at text-xl in a narrow
// 2-column mobile tile, that string is wider than the tile and the
// browser's own line-break lands wherever, sometimes stranding a lone
// "PM" on its own line. Breaking it ourselves, at the one place that
// always reads cleanly (between date and time), avoids that.
function formatTimestampTile(v: number): ReactNode {
  const d = new Date(v * 1000)

  return (
    <>
      {d.toLocaleDateString()}
      <br />
      {d.toLocaleTimeString()}
    </>
  )
}

const TILE_METRICS: { metric: string; labelKey: string; format: (v: number) => ReactNode; valueClassName?: string }[] = [
  { metric: 'am4_company_rank', labelKey: 'metrics.tiles.rank', format: (v) => Math.round(v).toLocaleString() },
  { metric: 'am4_ac_fleet_size', labelKey: 'metrics.tiles.fleetSize', format: (v) => Math.round(v).toLocaleString() },
  { metric: 'am4_company_share_value', labelKey: 'metrics.tiles.sharePrice', format: (v) => v.toFixed(2) },
  {
    metric: 'am4_stats_flights_operated_total',
    labelKey: 'metrics.tiles.flightsOperated',
    format: (v) => Math.round(v).toLocaleString(),
  },
  { metric: 'am4_duration_seconds', labelKey: 'metrics.tiles.lastRunSeconds', format: (v) => Math.round(v).toString() },
  // am4_last_run_timestamp_seconds is set at the end of every completed
  // cycle (internal/bot/bot.go) -- distinct from am4_duration_seconds
  // above, which is how LONG that cycle took, not WHEN it happened.
  {
    metric: 'am4_last_run_timestamp_seconds',
    labelKey: 'metrics.tiles.lastRunAt',
    valueClassName: 'text-sm font-medium leading-tight',
    format: formatTimestampTile,
  },
  // am4_next_scheduled_run_timestamp_seconds is set by cmd/ambot's own
  // cron scheduler (not internal/bot -- see that metric's own doc
  // comment in internal/metrics/prometheus.go), correct for any schedule
  // shape. This is also what prometheus/alerts.yml's AmbotMissedSchedule
  // rule compares against time() -- seeing this tile visibly stuck in the
  // past on the page is the same signal that alert fires on.
  {
    metric: 'am4_next_scheduled_run_timestamp_seconds',
    labelKey: 'metrics.tiles.nextRunAt',
    valueClassName: 'text-sm font-medium leading-tight',
    format: formatTimestampTile,
  },
]

interface NodeForTiles {
  id: number
  name: string
}

export function NodeMetricTiles({ nodes, series }: { nodes: NodeForTiles[]; series: MetricSeries[] }) {
  const { t } = useTranslation()

  const upByNode = new Map<number, number>()
  for (const s of series) {
    if (s.metric === 'up' && s.node_id !== undefined) {
      upByNode.set(s.node_id, s.value)
    }
  }

  function valueFor(nodeId: number, metric: string): number | undefined {
    return series.find((s) => s.node_id === nodeId && s.metric === metric)?.value
  }

  if (nodes.length === 0) {
    return <p className="text-muted-foreground">{t('metrics.noNodes')}</p>
  }

  return (
    <>
      {nodes.map((node) => {
        const up = upByNode.get(node.id)

        return (
          <Card className="node-card" key={node.id}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                {up !== undefined && (
                  <span
                    className={cn('status-dot inline-block size-2.5 rounded-full', up === 1 ? 'bg-success' : 'bg-destructive')}
                    aria-label={up === 1 ? t('metrics.status.up') : t('metrics.status.down')}
                  />
                )}
                {node.name}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
                {TILE_METRICS.map(({ metric, labelKey, format, valueClassName }) => {
                  const value = valueFor(node.id, metric)

                  return (
                    <div key={metric}>
                      <div className="text-xs text-muted-foreground">{t(labelKey)}</div>
                      <div className={cn('text-xl font-semibold', valueClassName)}>
                        {value === undefined ? '—' : format(value)}
                      </div>
                    </div>
                  )
                })}
              </div>
            </CardContent>
          </Card>
        )
      })}
    </>
  )
}
