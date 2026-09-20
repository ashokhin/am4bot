import { api } from './client'
import type { LoginActivityEntry, LoginRequest, SetMyDisplayNameRequest, SetMyPasswordRequest, User } from './types'

export const authApi = {
  login: (req: LoginRequest) => api.post<User>('/api/auth/login', req),
  logout: () => api.post<void>('/api/auth/logout'),
  me: () => api.get<User>('/api/me'),
  setMyPassword: (req: SetMyPasswordRequest) => api.put<void>('/api/me/password', req),
  setMyDisplayName: (req: SetMyDisplayNameRequest) => api.put<void>('/api/me/display-name', req),
  myLoginActivity: () => api.get<LoginActivityEntry[]>('/api/me/login-activity'),
}
