import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { ProtectedRoute } from './ProtectedRoute'

/**
 * Like ProtectedRoute, but the mirror of AdminRoute: refuses an admin
 * caller instead of a non-admin one. An admin has no nodes/metrics/
 * settings of their own -- see requireNonAdminUser's Go-side doc comment
 * -- so hitting one of those routes bounces them to their own landing
 * page instead.
 */
export function NonAdminRoute({ children }: { children: ReactNode }) {
  const { user } = useAuth()

  return (
    <ProtectedRoute>
      {user?.is_admin ? <Navigate to="/users" replace /> : children}
    </ProtectedRoute>
  )
}
