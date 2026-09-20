import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { adminApi } from '../api/admin'
import type { AdminUserDetail } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { LoginActivityTable } from '../components/LoginActivityTable'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent } from '../components/ui/card'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../components/ui/dialog'
import { Label } from '../components/ui/label'
import { PasswordInput } from '../components/ui/password-input'
import { Switch } from '../components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'

export function AdminUserDetailPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { uuid } = useParams()
  const { user: me } = useAuth()
  const [user, setUser] = useState<AdminUserDetail | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [resetDialogOpen, setResetDialogOpen] = useState(false)
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false)
  const [unlocking, setUnlocking] = useState(false)

  function load() {
    adminApi
      .getUserDetail(uuid!)
      .then(setUser)
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }

  useEffect(load, [uuid])

  async function handleToggleDisabled() {
    if (!user) return

    try {
      await adminApi.setUserDisabled(user.uuid, !user.disabled)
      load()
    } catch {
      toast.error(t('users.errors.updateFailed'))
    }
  }

  async function handleUnlock() {
    if (!user) return

    setUnlocking(true)

    try {
      await adminApi.unlockUser(user.uuid)
      toast.success(t('adminUserDetail.unlocked'))
      load()
    } catch {
      toast.error(t('adminUserDetail.errors.unlockFailed'))
    } finally {
      setUnlocking(false)
    }
  }

  async function handleDelete() {
    if (!user) return

    try {
      await adminApi.deleteUser(user.uuid)
      toast.success(t('adminUserDetail.deleted', { login: user.login }))
      navigate('/users')
    } catch {
      toast.error(t('adminUserDetail.errors.deleteFailed'))
    }
  }

  if (loading) {
    return <p className="text-muted-foreground">{t('common.loading')}</p>
  }
  if (loadError || !user) {
    return (
      <p role="alert" className="text-destructive">
        {t('adminUserDetail.errors.loadFailed')}
      </p>
    )
  }

  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{user.display_name ?? user.login}</h1>
        <div className="flex items-center gap-2">
          {user.ban && <Badge variant="destructive">{t('adminUserDetail.locked')}</Badge>}
          <Badge variant={user.disabled ? 'secondary' : 'success'}>
            {user.disabled ? t('users.status.disabled') : t('users.status.active')}
          </Badge>
        </div>
      </div>

      {user.ban && (
        <Card>
          <CardContent className="flex flex-wrap items-center justify-between gap-4 pt-6">
            <div>
              <Label>{t('adminUserDetail.lockedSince')}</Label>
              <p className="text-sm whitespace-nowrap">{new Date(user.ban.banned_at).toLocaleString()}</p>
              <Label className="mt-2 block">{t('adminUserDetail.lockedUntil')}</Label>
              <p className="text-sm whitespace-nowrap">{new Date(user.ban.unban_at).toLocaleString()}</p>
              <Label className="mt-2 block">{t('adminUserDetail.lockReason')}</Label>
              <p className="text-sm text-muted-foreground">{user.ban.reason}</p>
            </div>
            <Button variant="outline" disabled={unlocking} onClick={() => void handleUnlock()}>
              {t('adminUserDetail.unlock')}
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="grid grid-cols-1 gap-4 pt-6 sm:grid-cols-2">
          <div>
            <Label>{t('common.login')}</Label>
            <p className="text-sm">{user.login}</p>
          </div>
          <div>
            <Label>{t('users.table.uuid')}</Label>
            <p className="font-mono text-xs">{user.uuid}</p>
          </div>
          <div>
            <Label>{t('users.table.lastLogin')}</Label>
            <p className="text-sm whitespace-nowrap">
              {user.last_login_at ? new Date(user.last_login_at).toLocaleString() : t('users.neverLoggedIn')}
            </p>
            {user.last_login_ip && (
              <p className="font-mono text-xs text-muted-foreground">
                {user.last_login_ip} &middot; {user.last_login_user_agent}
              </p>
            )}
          </div>
          <div>
            <Label>{t('settings.vpnRegion')}</Label>
            <p className="text-sm">{user.vpn_region_name ?? t('settings.vpnRegionNone')}</p>
          </div>
          <div>
            <Label>{t('adminUserDetail.lastFailedLogin')}</Label>
            <p className="text-sm whitespace-nowrap">
              {user.last_failed_login_at ? new Date(user.last_failed_login_at).toLocaleString() : t('users.neverLoggedIn')}
            </p>
            {user.last_failed_login_ip && (
              <p className="font-mono text-xs text-muted-foreground">
                {user.last_failed_login_ip} &middot; {user.last_failed_login_user_agent}
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="flex flex-wrap items-center gap-4 pt-6">
          <div className="flex items-center gap-2">
            <Switch
              checked={!user.disabled}
              disabled={user.uuid === me?.uuid}
              onCheckedChange={() => void handleToggleDisabled()}
              title={user.uuid === me?.uuid ? t('users.cannotDisableSelf') : undefined}
            />
            <span className="text-sm">{user.disabled ? t('users.actions.enable') : t('users.actions.disable')}</span>
          </div>
          <Button
            variant="outline"
            disabled={user.uuid === me?.uuid}
            title={user.uuid === me?.uuid ? t('adminUserDetail.cannotResetOwnPassword') : undefined}
            onClick={() => setResetDialogOpen(true)}
          >
            {t('adminUserDetail.resetPassword')}
          </Button>
          <Button
            variant="destructive"
            disabled={user.uuid === me?.uuid}
            title={user.uuid === me?.uuid ? t('adminUserDetail.cannotDeleteSelf') : undefined}
            onClick={() => setConfirmDeleteOpen(true)}
          >
            {t('adminUserDetail.deleteUser')}
          </Button>
        </CardContent>
      </Card>

      <div className="flex flex-col gap-2">
        <h2 className="text-lg font-semibold">{t('adminUserDetail.nodesSection')}</h2>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('nodes.table.name')}</TableHead>
              <TableHead>{t('nodes.table.status')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {user.nodes.map((node) => (
              <TableRow key={node.id} className="cursor-pointer" onClick={() => navigate(`/admin/nodes/${node.id}`)}>
                <TableCell className="font-medium">
                  {node.name}
                  {node.is_default && (
                    <Badge variant="secondary" className="ml-2">
                      {t('nodes.defaultBadge')}
                    </Badge>
                  )}
                </TableCell>
                <TableCell>
                  <Badge variant={node.enabled ? 'success' : 'secondary'}>
                    {node.enabled ? t('nodes.status.enabled') : t('nodes.status.disabled')}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className="flex flex-col gap-2">
        <h2 className="text-lg font-semibold">{t('loginActivity.title')}</h2>
        <LoginActivityTable entries={user.login_activity} />
      </div>

      <ResetPasswordDialog
        open={resetDialogOpen}
        onOpenChange={setResetDialogOpen}
        onReset={async (password) => {
          await adminApi.resetUserPassword(user.uuid, { password })
          setResetDialogOpen(false)
          toast.success(t('adminUserDetail.passwordReset'))
        }}
      />

      <ConfirmDialog
        open={confirmDeleteOpen}
        onOpenChange={setConfirmDeleteOpen}
        title={t('adminUserDetail.deleteUser')}
        description={t('adminUserDetail.confirmDelete', { login: user.login })}
        destructive
        onConfirm={() => void handleDelete()}
      />
    </div>
  )
}

function ResetPasswordDialog({
  open,
  onOpenChange,
  onReset,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onReset: (password: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const [password, setPassword] = useState('')
  const [error, setError] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(false)
    setSubmitting(true)

    try {
      await onReset(password)
      setPassword('')
    } catch {
      setError(true)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('adminUserDetail.resetPassword')}</DialogTitle>
        </DialogHeader>
        <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="reset-password">{t('adminUserDetail.newPassword')}</Label>
            <PasswordInput
              id="reset-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              autoFocus
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {t('adminUserDetail.errors.resetPasswordFailed')}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={submitting}>
              {t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
