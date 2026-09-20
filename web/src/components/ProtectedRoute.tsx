import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth/AuthContext'

/**
 * Wraps a page that requires a logged-in user, redirecting to /login
 * otherwise. Also forces every other route to /change-password while the
 * signed-in user still has an admin-set (never self-chosen) password --
 * see User.must_change_password's doc comment. /change-password itself
 * is wrapped in this same guard, so the check must let that one path
 * through or every visit there would immediately bounce back to itself.
 */
export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { user, ready } = useAuth()
  const { t } = useTranslation()
  const location = useLocation()

  if (!ready) {
    return <p>{t('common.loading')}</p>
  }

  if (!user) {
    return <Navigate to="/login" replace />
  }

  if (user.must_change_password && location.pathname !== '/change-password') {
    return <Navigate to="/change-password" replace />
  }

  return <>{children}</>
}
