import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminApi } from '../api/admin'
import { metricsApi } from '../api/metrics'
import type { AdminUserListEntry, MetricSeries, Node } from '../api/types'
import { LastUpdated } from '../components/LastUpdated'
import { NodeMetricTiles } from '../components/NodeMetricTiles'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select'
import { usePolling } from '../hooks/usePolling'

// Admin's cross-user view of MetricsPage: pick a user, see their nodes'
// tiles. The metrics endpoint only returns node_id + value, no node
// names, so node names come from the same getUserDetail call used by
// AdminUserDetailPage.
export function AdminMetricsPage() {
  const { t } = useTranslation()
  const [users, setUsers] = useState<AdminUserListEntry[]>([])
  const [usersLoading, setUsersLoading] = useState(true)
  const [usersLoadError, setUsersLoadError] = useState(false)
  const [selectedUuid, setSelectedUuid] = useState<string | undefined>(undefined)

  const [nodes, setNodes] = useState<Node[]>([])
  const [series, setSeries] = useState<MetricSeries[]>([])
  const [configured, setConfigured] = useState(true)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailLoadError, setDetailLoadError] = useState(false)
  const [lastUpdatedAt, setLastUpdatedAt] = useState<Date | null>(null)

  useEffect(() => {
    adminApi
      .listUsers()
      .then((list) => {
        setUsers(list)
        if (list.length > 0) {
          setSelectedUuid(list[0].uuid)
        }
      })
      .catch(() => setUsersLoadError(true))
      .finally(() => setUsersLoading(false))
  }, [])

  // usePolling's fn also fires on every periodic tick, not just when
  // selectedUuid actually changes -- this distinguishes "a fresh user was
  // just picked" (reset to a loading state, previous user's tiles
  // shouldn't linger) from "just another routine re-poll of the same
  // user" (keep showing the current data while the request is in flight,
  // no loading flicker every 20s).
  const prevUuidRef = useRef<string | undefined>(undefined)

  usePolling(() => {
    if (!selectedUuid) return

    if (prevUuidRef.current !== selectedUuid) {
      prevUuidRef.current = selectedUuid
      setDetailLoading(true)
      setDetailLoadError(false)
      setLastUpdatedAt(null)
    }

    Promise.all([adminApi.getUserDetail(selectedUuid), metricsApi.getForUser(selectedUuid)])
      .then(([user, m]) => {
        setNodes(user.nodes)
        setConfigured(m.configured)
        setSeries(m.series ?? [])
        setLastUpdatedAt(new Date())
        setDetailLoadError(false)
      })
      .catch(() => {
        // Same "only a first-load failure is fatal" rationale as
        // MetricsPage -- a transient poll hiccup shouldn't blank out
        // already-displayed data.
        setDetailLoadError((prev) => prev || lastUpdatedAt === null)
      })
      .finally(() => setDetailLoading(false))
  }, [selectedUuid])

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-baseline justify-between gap-2">
        <h1 className="text-2xl font-semibold">{t('metrics.title')}</h1>
        <LastUpdated at={lastUpdatedAt} />
      </div>

      {usersLoading && <p className="text-muted-foreground">{t('common.loading')}</p>}
      {usersLoadError && (
        <p role="alert" className="text-destructive">
          {t('users.errors.loadFailed')}
        </p>
      )}

      {!usersLoading && !usersLoadError && (
        <Select value={selectedUuid} onValueChange={setSelectedUuid}>
          <SelectTrigger className="w-full sm:w-80" data-testid="admin-metrics-user">
            <SelectValue placeholder={t('adminMetrics.selectUser')} />
          </SelectTrigger>
          <SelectContent>
            {users.map((u) => (
              <SelectItem key={u.uuid} value={u.uuid}>
                {u.login}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      {selectedUuid && detailLoading && <p className="text-muted-foreground">{t('common.loading')}</p>}
      {selectedUuid && detailLoadError && (
        <p role="alert" className="text-destructive">
          {t('metrics.errors.loadFailed')}
        </p>
      )}
      {selectedUuid && !detailLoading && !detailLoadError && !configured && (
        <p className="text-muted-foreground">{t('metrics.notConfigured')}</p>
      )}
      {selectedUuid && !detailLoading && !detailLoadError && configured && (
        <NodeMetricTiles nodes={nodes} series={series} />
      )}
    </div>
  )
}
