import { api } from './client'
import type { CreateNodeRequest, Node, UpdateNodeRequest } from './types'

export const nodesApi = {
  list: () => api.get<Node[]>('/api/nodes'),
  get: (id: number) => api.get<Node>(`/api/nodes/${id}`),
  create: (req: CreateNodeRequest) => api.post<Node>('/api/nodes', req),
  update: (id: number, req: UpdateNodeRequest) => api.patch<Node>(`/api/nodes/${id}`, req),
  remove: (id: number) => api.delete<void>(`/api/nodes/${id}`),
}
