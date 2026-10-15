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
  /** Sets the signed-in user straight from a finished passkey login ceremony - LoginPage's "sign in with passkey" button already has the User the server returned, no need to re-fetch /api/me. */
  loginWithPasskey: (loggedInUser: User) => void
  logout: () => Promise<void>
  /** Re-fetches /api/me - call after a self-service change (e.g. picking a VPN region) so the rest of the app sees the new state without a full reload. */
  refreshUser: () => Promise<void>
  /** Clears show_passkey_prompt locally once the post-login prompt has been handled (added a passkey, or skipped) - avoids an extra /api/me round trip just to make the modal stay closed. */
  clearPasskeyPrompt: () => void
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
        // 401 just means "not logged in yet" - not worth surfacing.
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

  const loginWithPasskey = useCallback((loggedInUser: User) => {
    setUser(loggedInUser)
  }, [])

  const logout = useCallback(async () => {
    await authApi.logout()
    setUser(undefined)
  }, [])

  const refreshUser = useCallback(async () => {
    setUser(await authApi.me())
  }, [])

  const clearPasskeyPrompt = useCallback(() => {
    setUser((u) => (u ? { ...u, show_passkey_prompt: false } : u))
  }, [])

  return (
    <AuthContext.Provider value={{ user, ready, login, loginWithPasskey, logout, refreshUser, clearPasskeyPrompt }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth() must be called within an AuthProvider')
  }

  return ctx
}
