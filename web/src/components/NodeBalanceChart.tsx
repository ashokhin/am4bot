import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  type TooltipContentProps,
  XAxis,
  YAxis,
} from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { DELTA_PERIODS, type DeltaPeriod } from '../api/metrics'
import type { MetricSeries } from '../api/types'
import { Card, CardContent, CardHeader, CardTitle } from './ui/card'
import { Tabs, TabsList, TabsTrigger } from './ui/tabs'

// Company balance over time -- GET /api/metrics/balance (see
// internal/api/metrics_handlers.go's balanceMetric/queryBalanceRange),
// one am4_company_money{type="Airline account"} query_range call per
// selected period. Follows the dataviz skill's line-chart contract: 2px
// lines, a shared crosshair tooltip listing every node at that point in
// time, a legend only once there's more than one node (color is then the
// only way to tell nodes apart), and the eight-hue categorical palette
// (--chart-series-1..8 in index.css) assigned by node id -- fixed order,
// never re-assigned by value/rank.

const SERIES_COLOR_COUNT = 8

function colorForIndex(index: number): string {
  return `var(--chart-series-${(index % SERIES_COLOR_COUNT) + 1})`
}

// Compact notation ("34.8B") -- these balances run into the billions,
// and the y-axis/tooltip both need to stay readable at a glance rather
// than showing a wall of digits.
const compactFormatter = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 })

interface NodeForChart {
  id: number
  name: string
}

interface ChartRow {
  timestamp: number
  [nodeKey: string]: number
}

function nodeKey(nodeId: number): string {
  return `node_${nodeId}`
}

// Merges every node's [timestamp, value] points onto one array of rows
// keyed by timestamp -- query_range returns every node on the same step
// grid for a single request, so timestamps line up across nodes; a node
// with a shorter history (e.g. just provisioned) simply has no key at
// the earlier rows, which Recharts renders as a gap (no connectNulls),
// not a false zero.
function buildChartData(series: MetricSeries[]): ChartRow[] {
  const byTimestamp = new Map<number, ChartRow>()

  for (const point of series) {
    if (point.node_id === undefined) continue

    const row = byTimestamp.get(point.timestamp) ?? { timestamp: point.timestamp }
    row[nodeKey(point.node_id)] = point.value
    byTimestamp.set(point.timestamp, row)
  }

  return [...byTimestamp.values()].sort((a, b) => a.timestamp - b.timestamp)
}

function formatAxisTime(timestamp: number, period: DeltaPeriod): string {
  const d = new Date(timestamp * 1000)

  return period === '24h' ? d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString()
}

// Custom tooltip per the dataviz skill's interaction contract: value
// leads (bold), series name follows (secondary ink), each row keyed by a
// short line-stroke of the series color rather than a filled box, and
// every series at that timestamp is listed together rather than
// requiring the pointer to land exactly on one line.
function BalanceTooltip({
  active,
  payload,
  label,
  period,
  nodesByKey,
}: TooltipContentProps<ValueType, NameType> & { period: DeltaPeriod; nodesByKey: Map<string, NodeForChart> }) {
  if (!active || !payload || payload.length === 0) return null

  return (
    <div className="rounded-md border bg-popover px-3 py-2 text-sm shadow-md">
      <div className="mb-1 text-xs text-muted-foreground">{formatAxisTime(Number(label), period)}</div>
      {payload
        .filter((p) => p.value !== undefined)
        .map((p) => {
          const node = nodesByKey.get(String(p.dataKey))

          return (
            <div key={String(p.dataKey)} className="flex items-center gap-2">
              <span className="inline-block h-0.5 w-3 shrink-0" style={{ backgroundColor: p.color }} />
              <span className="font-semibold text-popover-foreground">{compactFormatter.format(Number(p.value))}</span>
              <span className="text-muted-foreground">{node?.name}</span>
            </div>
          )
        })}
    </div>
  )
}

export function NodeBalanceChart({
  nodes,
  series,
  period,
  onPeriodChange,
}: {
  nodes: NodeForChart[]
  series: MetricSeries[]
  period: DeltaPeriod
  onPeriodChange: (period: DeltaPeriod) => void
}) {
  const { t } = useTranslation()

  // Sorted by id, not by however the API happened to order this poll's
  // response -- color identity must stay fixed per node across renders,
  // never reshuffled by rank/position.
  const sortedNodes = useMemo(() => [...nodes].sort((a, b) => a.id - b.id), [nodes])
  const nodesByKey = useMemo(() => new Map(sortedNodes.map((n) => [nodeKey(n.id), n])), [sortedNodes])
  const chartData = useMemo(() => buildChartData(series), [series])

  if (nodes.length === 0) {
    return null
  }

  return (
    <Card className="node-balance-card">
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle className="text-base">{t('metrics.balance.title')}</CardTitle>
        <Tabs value={period} onValueChange={(v) => onPeriodChange(v as DeltaPeriod)}>
          <TabsList>
            {DELTA_PERIODS.map((p) => (
              <TabsTrigger key={p} value={p} data-testid={`balance-period-${p}`}>
                {t(`metrics.delta.period.${p}`)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </CardHeader>
      <CardContent>
        {chartData.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('metrics.balance.noData')}</p>
        ) : (
          <>
            <ResponsiveContainer width="100%" height={280}>
              <LineChart data={chartData} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                <CartesianGrid vertical={false} stroke="var(--border)" />
                <XAxis
                  dataKey="timestamp"
                  tickFormatter={(v: number) => formatAxisTime(v, period)}
                  stroke="var(--muted-foreground)"
                  tick={{ fontSize: 12 }}
                  tickLine={false}
                  axisLine={{ stroke: 'var(--border)' }}
                  minTickGap={40}
                />
                <YAxis
                  // Recharts' own default domain is [0, 'auto'] -- fine
                  // for a value that swings near zero, but a balance in
                  // the billions with day-to-day changes in the millions
                  // looks like a flat line forced to share the chart with
                  // a zero baseline it never approaches. 'auto' on both
                  // ends fits the visible range instead, so the actual
                  // movement is what fills the chart.
                  domain={['auto', 'auto']}
                  tickFormatter={(v: number) => compactFormatter.format(v)}
                  stroke="var(--muted-foreground)"
                  tick={{ fontSize: 12 }}
                  tickLine={false}
                  axisLine={false}
                  width={56}
                />
                <Tooltip content={(props) => <BalanceTooltip {...props} period={period} nodesByKey={nodesByKey} />} />
                {sortedNodes.map((node, i) => (
                  <Line
                    key={node.id}
                    dataKey={nodeKey(node.id)}
                    name={node.name}
                    stroke={colorForIndex(i)}
                    strokeWidth={2}
                    dot={false}
                    activeDot={{ r: 4 }}
                    connectNulls={false}
                    isAnimationActive={false}
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
            {sortedNodes.length > 1 && (
              <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
                {sortedNodes.map((node, i) => (
                  <div key={node.id} className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span className="inline-block h-0.5 w-3" style={{ backgroundColor: colorForIndex(i) }} />
                    {node.name}
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}
