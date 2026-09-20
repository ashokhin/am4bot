import { api } from './client'
import type { MetricsResponse, PrometheusSettingsStatus, SetPrometheusSettingsRequest } from './types'

export const metricsApi = {
  // The current user's own metrics -- the server filters by their uuid,
  // there is no client-supplied scope here.
  get: () => api.get<MetricsResponse>('/api/metrics'),
  // Admin-only: any user's metrics, by uuid -- see
  // internal/api/metrics_handlers.go's handleAdminGetMetrics.
  getForUser: (userUUID: string) =>
    api.get<MetricsResponse>(`/api/admin/metrics?user_uuid=${encodeURIComponent(userUUID)}`),
  getPrometheusSettings: () => api.get<PrometheusSettingsStatus>('/api/admin/prometheus'),
  setPrometheusSettings: (req: SetPrometheusSettingsRequest) =>
    api.put<PrometheusSettingsStatus>('/api/admin/prometheus', req),
}
