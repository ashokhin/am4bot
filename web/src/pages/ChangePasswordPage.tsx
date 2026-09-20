import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { authApi } from '../api/auth'
import { useAuth } from '../auth/AuthContext'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Label } from '../components/ui/label'
import { PasswordInput } from '../components/ui/password-input'

// Forced landing spot for a user whose password is still the one an
// admin set for them (account creation or a reset) -- see
// User.must_change_password's doc comment and ProtectedRoute, which
// redirects every other route here until this succeeds. Reuses the same
// PUT /api/me/password endpoint as the voluntary change on SettingsPage
// -- the backend clears must_change_password on any successful
// self-service change, forced or not. No current-password field: the
// caller just typed it seconds ago to log in, so re-asking is pure
// friction -- see handleSetMyPassword's doc comment on the Go side.
export function ChangePasswordPage() {
  const { t } = useTranslation()
  const { refreshUser } = useAuth()
  const navigate = useNavigate()

  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState<string | undefined>(undefined)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(undefined)

    if (newPassword !== confirmPassword) {
      setError(t('forceChangePassword.errors.mismatch'))

      return
    }

    setSubmitting(true)

    try {
      await authApi.setMyPassword({ new_password: newPassword })
      await refreshUser()
      navigate('/nodes')
    } catch {
      setError(t('forceChangePassword.errors.failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>{t('forceChangePassword.title')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">{t('forceChangePassword.intro')}</p>

          <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="new-password">{t('settings.newPassword')}</Label>
              <PasswordInput
                id="new-password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                required
                autoFocus
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="confirm-password">{t('settings.confirmPassword')}</Label>
              <PasswordInput
                id="confirm-password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                required
              />
            </div>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <Button type="submit" disabled={submitting}>
              {t('forceChangePassword.submit')}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
