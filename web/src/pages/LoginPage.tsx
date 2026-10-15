import { startAuthentication } from '@simplewebauthn/browser'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate } from 'react-router-dom'
import { passkeysApi } from '../api/passkeys'
import { useAuth } from '../auth/AuthContext'
import { LanguageSwitcher } from '../components/LanguageSwitcher'
import { ThemeToggle } from '../components/ThemeToggle'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { PasswordInput } from '../components/ui/password-input'

export function LoginPage() {
  const { t } = useTranslation()
  const { user, login, loginWithPasskey } = useAuth()
  const [loginValue, setLoginValue] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState(false)
  const [passkeyError, setPasskeyError] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [passkeySubmitting, setPasskeySubmitting] = useState(false)

  if (user) {
    return <Navigate to="/nodes" replace />
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(false)
    setSubmitting(true)

    try {
      await login(loginValue, password)
    } catch {
      setError(true)
    } finally {
      setSubmitting(false)
    }
  }

  // Discoverable/usernameless login: the user never types a login, the
  // authenticator itself tells the server (via loginFinish) who's signing
  // in - see internal/api/passkey_handlers.go's handlePasskeyLoginBegin.
  async function handlePasskeyLogin() {
    setPasskeyError(false)
    setPasskeySubmitting(true)

    try {
      const { flow_id, assertion } = await passkeysApi.loginBegin()
      const response = await startAuthentication({ optionsJSON: assertion.publicKey })
      const loggedInUser = await passkeysApi.loginFinish(flow_id, response)
      loginWithPasskey(loggedInUser)
    } catch {
      setPasskeyError(true)
    } finally {
      setPasskeySubmitting(false)
    }
  }

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 p-4">
      <div className="flex w-full max-w-sm items-center justify-between">
        <span className="text-lg font-bold">am4bot</span>
        <div className="flex items-center gap-1">
          <LanguageSwitcher />
          <ThemeToggle />
        </div>
      </div>
      <Card className="w-full max-w-sm">
        <CardHeader>
          <h1 className="text-xl leading-none font-semibold">{t('login.title')}</h1>
        </CardHeader>
        <CardContent>
          <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="login">{t('common.login')}</Label>
              <Input
                id="login"
                type="text"
                value={loginValue}
                onChange={(e) => setLoginValue(e.target.value)}
                required
                autoFocus
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="password">{t('common.password')}</Label>
              <PasswordInput
                id="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {t('login.error')}
              </p>
            )}
            <Button type="submit" disabled={submitting} className="mt-1">
              {t('login.submit')}
            </Button>
          </form>
          <div className="mt-4 flex flex-col gap-1.5">
            {passkeyError && (
              <p role="alert" className="text-sm text-destructive">
                {t('login.passkeyError')}
              </p>
            )}
            <Button
              type="button"
              variant="outline"
              disabled={passkeySubmitting}
              onClick={() => void handlePasskeyLogin()}
            >
              {t('login.withPasskey')}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
