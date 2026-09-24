import { api } from './client'
import type { MetricsResponse, PrometheusSettingsStatus, SetPrometheusSettingsRequest } from './types'

// The delta widget's period buttons -- must match deltaPeriods in
// internal/api/metrics_handlers.go exactly, the server rejects anything
// else.
export const DELTA_PERIODS = ['24h', '3d', '7d', '14d', '30d'] as const
export type DeltaPeriod = (typeof DELTA_PERIODS)[number]

export const metricsApi = {
  // The current user's own metrics -- the server filters by their uuid,
  // there is no client-supplied scope here.
  get: () => api.get<MetricsResponse>('/api/metrics'),
  // "How much changed over the last `period`" -- see deltaMetrics/
  // deltaPeriods in internal/api/metrics_handlers.go.
  getDelta: (period: DeltaPeriod) => api.get<MetricsResponse>(`/api/metrics/delta?period=${period}`),
  // Admin-only: any user's metrics, by uuid -- see
  // internal/api/metrics_handlers.go's handleAdminGetMetrics.
  getForUser: (userUUID: string) =>
    api.get<MetricsResponse>(`/api/admin/metrics?user_uuid=${encodeURIComponent(userUUID)}`),
  getDeltaForUser: (userUUID: string, period: DeltaPeriod) =>
    api.get<MetricsResponse>(`/api/admin/metrics/delta?user_uuid=${encodeURIComponent(userUUID)}&period=${period}`),
  // Balance-over-time chart -- see balanceMetric/queryBalanceRange in
  // internal/api/metrics_handlers.go.
  getBalance: (period: DeltaPeriod) => api.get<MetricsResponse>(`/api/metrics/balance?period=${period}`),
  getBalanceForUser: (userUUID: string, period: DeltaPeriod) =>
    api.get<MetricsResponse>(`/api/admin/metrics/balance?user_uuid=${encodeURIComponent(userUUID)}&period=${period}`),
  getPrometheusSettings: () => api.get<PrometheusSettingsStatus>('/api/admin/prometheus'),
  setPrometheusSettings: (req: SetPrometheusSettingsRequest) =>
    api.put<PrometheusSettingsStatus>('/api/admin/prometheus', req),
}
