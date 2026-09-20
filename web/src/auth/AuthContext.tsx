import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { ApiError } from '../api/client'
import { authApi } from '../api/auth'
import type { User } from '../api/types'

interface AuthState {
  /** undefined while the initial /api/me check is in flight. */
  user: User | undefined
  /** true once the initial /api/me check has resolved (either way). */
  ready: boolean
  login: (login: string, password: string) => Promise<void>
  logout: () => Promise<void>
  /** Re-fetches /api/me -- call after a self-service change (e.g. picking a VPN region) so the rest of the app sees the new state without a full reload. */
  refreshUser: () => Promise<void>
}

const AuthContext = createContext<AuthState | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | undefined>(undefined)
  const [ready, setReady] = useState(false)

  useEffect(() => {
    authApi
      .me()
      .then(setUser)
      .catch((err: unknown) => {
        // 401 just means "not logged in yet" -- not worth surfacing.
        if (!(err instanceof ApiError && err.status === 401)) {
          console.error('checking current session', err)
        }
      })
      .finally(() => setReady(true))
  }, [])

  const login = useCallback(async (login: string, password: string) => {
    const loggedInUser = await authApi.login({ login, password })
    setUser(loggedInUser)
  }, [])

  const logout = useCallback(async () => {
    await authApi.logout()
    setUser(undefined)
  }, [])

  const refreshUser = useCallback(async () => {
    setUser(await authApi.me())
  }, [])

  return <AuthContext.Provider value={{ user, ready, login, logout, refreshUser }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth() must be called within an AuthProvider')
  }

  return ctx
}
