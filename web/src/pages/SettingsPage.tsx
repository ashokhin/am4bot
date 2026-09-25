import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { authApi } from '../api/auth'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { vpnRegionsApi } from '../api/vpnRegions'
import type { LoginActivityEntry, VPNRegion } from '../api/types'
import { LoginActivityTable } from '../components/LoginActivityTable'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { PasswordInput } from '../components/ui/password-input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select'

const NONE = '__none__'

/**
 * Every signed-in user's own account settings -- admin included (display
 * name + password change apply to everyone). VPN region is the one
 * section that's non-admin-only: an admin has no nodes of their own to
 * apply it to (see internal/store/vpn_regions.go's doc comment for the
 * per-user, applies-to-every-node model).
 */
export function SettingsPage() {
  const { t } = useTranslation()
  const { user } = useAuth()

  return (
    <div className="flex max-w-6xl flex-col gap-4">
      <h1 className="text-2xl font-semibold">{t('settings.title')}</h1>

      <AccountSection />

      {!user?.is_admin && <VPNRegionSection />}

      <LoginActivitySection />
    </div>
  )
}

function LoginActivitySection() {
  const { t } = useTranslation()
  const [entries, setEntries] = useState<LoginActivityEntry[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    authApi
      .myLoginActivity()
      .then(setEntries)
      .catch(() => {
        /* best-effort -- an empty activity table is an acceptable degrade */
      })
      .finally(() => setLoading(false))
  }, [])

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t('loginActivity.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? <p className="text-sm text-muted-foreground">{t('common.loading')}</p> : <LoginActivityTable entries={entries} />}
      </CardContent>
    </Card>
  )
}

function AccountSection() {
  const { t } = useTranslation()
  const { user, refreshUser } = useAuth()

  const [displayName, setDisplayName] = useState(user?.display_name ?? '')
  const [savingName, setSavingName] = useState(false)

  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | undefined>(undefined)
  const [savingPassword, setSavingPassword] = useState(false)

  async function handleSaveName(e: FormEvent) {
    e.preventDefault()
    setSavingName(true)

    try {
      await authApi.setMyDisplayName({ display_name: displayName || null })
      await refreshUser()
      toast.success(t('settings.saved'))
    } catch {
      toast.error(t('settings.errors.saveFailed'))
    } finally {
      setSavingName(false)
    }
  }

  async function handleChangePassword(e: FormEvent) {
    e.preventDefault()
    setPasswordError(undefined)

    if (newPassword !== confirmPassword) {
      setPasswordError(t('settings.errors.passwordMismatch'))

      return
    }

    setSavingPassword(true)

    try {
      await authApi.setMyPassword({ new_password: newPassword })
      setNewPassword('')
      setConfirmPassword('')
      toast.success(t('settings.passwordChanged'))
    } catch {
      setPasswordError(t('settings.errors.passwordChangeFailed'))
    } finally {
      setSavingPassword(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t('settings.account')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <div className="flex flex-col gap-1.5">
          <Label>{t('common.login')}</Label>
          <p className="text-sm">{user?.login}</p>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label>{t('users.table.uuid')}</Label>
          <p className="font-mono text-xs text-muted-foreground">{user?.uuid}</p>
        </div>

        <form onSubmit={(e) => void handleSaveName(e)} className="flex flex-col gap-2">
          <Label htmlFor="display-name">{t('settings.displayName')}</Label>
          <div className="flex gap-2">
            <Input
              id="display-name"
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder={user?.login}
            />
            <Button type="submit" variant="outline" disabled={savingName}>
              {t('common.save')}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">{t('settings.displayNameHint')}</p>
        </form>

        <form onSubmit={(e) => void handleChangePassword(e)} className="flex flex-col gap-2">
          <Label>{t('settings.changePassword')}</Label>
          <PasswordInput
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder={t('settings.newPassword')}
            required
          />
          <PasswordInput
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            placeholder={t('settings.confirmPassword')}
            required
          />
          {passwordError && (
            <p role="alert" className="text-sm text-destructive">
              {passwordError}
            </p>
          )}
          <Button type="submit" variant="outline" disabled={savingPassword} className="w-fit">
            {t('settings.changePassword')}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function VPNRegionSection() {
  const { t } = useTranslation()
  const { user, refreshUser } = useAuth()
  const [regions, setRegions] = useState<VPNRegion[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    vpnRegionsApi
      .list()
      .then(setRegions)
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }, [])

  async function handleChange(value: string) {
    setSaving(true)

    try {
      await vpnRegionsApi.setMyRegion({ vpn_region_id: value === NONE ? null : Number(value) })
      await refreshUser()
      toast.success(t('settings.saved'))
    } catch (err) {
      // 409: the admin hasn't configured the shared VPN provider account
      // yet (see handleSetMyVPNRegion) -- nothing the user can fix.
      toast.error(err instanceof ApiError && err.status === 409 ? t('settings.errors.vpnNotConfigured') : t('settings.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t('settings.vpnRegion')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <Label htmlFor="vpn-region">{t('settings.vpnRegion')}</Label>
        <Select
          value={user?.vpn_region_id ? String(user.vpn_region_id) : NONE}
          disabled={loading || saving}
          onValueChange={(v) => void handleChange(v)}
        >
          <SelectTrigger id="vpn-region" data-testid="vpn-region" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>{t('settings.vpnRegionNone')}</SelectItem>
            {regions.map((r) => (
              <SelectItem key={r.id} value={String(r.id)}>
                {r.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-sm text-muted-foreground">{t('settings.vpnRegionHint')}</p>
        {loadError && (
          <p role="alert" className="text-sm text-destructive">
            {t('settings.errors.loadFailed')}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
