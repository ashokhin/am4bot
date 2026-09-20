import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { ProtectedRoute } from './ProtectedRoute'

/** Like ProtectedRoute, but also requires the logged-in user to be an admin. */
export function AdminRoute({ children }: { children: ReactNode }) {
  const { user } = useAuth()

  return (
    <ProtectedRoute>
      {user?.is_admin ? children : <Navigate to="/nodes" replace />}
    </ProtectedRoute>
  )
}
