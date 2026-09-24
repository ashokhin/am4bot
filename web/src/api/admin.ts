import { api } from './client'
import type {
  AdminNodeView,
  AdminUserDetail,
  AdminUserListEntry,
  CreateUserRequest,
  ResetUserPasswordRequest,
  SetNodeLogLevelRequest,
  UpdateAllNodesResponse,
  User,
} from './types'

export const adminApi = {
  listUsers: () => api.get<AdminUserListEntry[]>('/api/admin/users'),
  getUserDetail: (uuid: string) => api.get<AdminUserDetail>(`/api/admin/users/${uuid}`),
  createUser: (req: CreateUserRequest) => api.post<User>('/api/admin/users', req),
  setUserDisabled: (uuid: string, disabled: boolean) =>
    api.post<void>(`/api/admin/users/${uuid}/${disabled ? 'disable' : 'enable'}`),
  resetUserPassword: (uuid: string, req: ResetUserPasswordRequest) =>
    api.post<void>(`/api/admin/users/${uuid}/reset-password`, req),
  unlockUser: (uuid: string) => api.post<void>(`/api/admin/users/${uuid}/unlock`),
  deleteUser: (uuid: string) => api.delete<void>(`/api/admin/users/${uuid}`),
  listAllNodes: () => api.get<AdminNodeView[]>('/api/admin/nodes'),
  // Queues a reconcile for every enabled node so each re-pulls its image --
  // see internal/api/admin_handlers.go's handleUpdateAllNodes.
  updateAllNodes: () => api.post<UpdateAllNodesResponse>('/api/admin/nodes/update-all'),
  getNode: (id: number) => api.get<AdminNodeView>(`/api/admin/nodes/${id}`),
  setNodeLogLevel: (id: number, req: SetNodeLogLevelRequest) => api.put<void>(`/api/admin/nodes/${id}/log-level`, req),
}
