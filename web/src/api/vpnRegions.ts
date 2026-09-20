import { api } from './client'
import type {
  CreateVPNRegionRequest,
  SetUserVPNRegionRequest,
  SetVPNProviderRequest,
  VPNProviderStatus,
  VPNRegion,
} from './types'

// Readable by any signed-in user (to populate their own region picker);
// create/delete and the provider credentials are admin-only, enforced
// server-side, not just hidden in the UI.
export const vpnRegionsApi = {
  list: () => api.get<VPNRegion[]>('/api/vpn-regions'),
  create: (req: CreateVPNRegionRequest) => api.post<VPNRegion>('/api/admin/vpn-regions', req),
  remove: (id: number) => api.delete<void>(`/api/admin/vpn-regions/${id}`),
  getProviderStatus: () => api.get<VPNProviderStatus>('/api/admin/vpn-provider'),
  setProviderCredentials: (req: SetVPNProviderRequest) => api.put<VPNProviderStatus>('/api/admin/vpn-provider', req),
  setMyRegion: (req: SetUserVPNRegionRequest) => api.put<void>('/api/me/vpn-region', req),
}
