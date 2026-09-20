import { PlusIcon } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { adminApi } from '../api/admin'
import type { AdminUserListEntry } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../components/ui/dialog'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { PasswordInput } from '../components/ui/password-input'
import { Switch } from '../components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'

function formatLastLogin(iso: string | null, t: (key: string) => string): string {
  if (!iso) {
    return t('users.neverLoggedIn')
  }

  return new Date(iso).toLocaleString()
}

export function AdminUsersPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { user: me } = useAuth()
  const [users, setUsers] = useState<AdminUserListEntry[]>([])
  const [loadError, setLoadError] = useState(false)
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)

  function load() {
    adminApi
      .listUsers()
      .then(setUsers)
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  async function handleToggleDisabled(user: AdminUserListEntry) {
    try {
      await adminApi.setUserDisabled(user.uuid, !user.disabled)
      setUsers((prev) => prev.map((u) => (u.uuid === user.uuid ? { ...u, disabled: !u.disabled } : u)))
    } catch {
      toast.error(t('users.errors.updateFailed'))
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{t('users.title')}</h1>
        <Button onClick={() => setDialogOpen(true)}>
          <PlusIcon /> {t('users.addUser')}
        </Button>
      </div>

      {loading && <p className="text-muted-foreground">{t('common.loading')}</p>}
      {loadError && (
        <p role="alert" className="text-destructive">
          {t('users.errors.loadFailed')}
        </p>
      )}

      {!loading && !loadError && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('users.table.login')}</TableHead>
              {/* Hidden below sm -- each row already navigates to a
                  detail page with all of this, see the row's onClick
                  below. Not worth a permanent horizontal scroll for a
                  UUID/timestamp/count on a phone. */}
              <TableHead className="hidden sm:table-cell">{t('users.table.uuid')}</TableHead>
              <TableHead className="hidden sm:table-cell">{t('users.table.lastLogin')}</TableHead>
              <TableHead className="hidden sm:table-cell">{t('users.table.nodes')}</TableHead>
              <TableHead>{t('users.table.status')}</TableHead>
              <TableHead className="text-right">{t('users.table.actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((user) => {
              const isSelf = user.uuid === me?.uuid

              return (
                <TableRow
                  key={user.uuid}
                  className="cursor-pointer"
                  onClick={() => navigate(`/admin/users/${user.uuid}`)}
                >
                  <TableCell className="font-medium">
                    {user.login}
                    {user.display_name && <span className="ml-1 font-normal text-muted-foreground">({user.display_name})</span>}
                    {isSelf && (
                      <Badge variant="outline" className="ml-2">
                        {t('users.you')}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="hidden font-mono text-xs text-muted-foreground sm:table-cell">{user.uuid}</TableCell>
                  <TableCell className="hidden text-muted-foreground sm:table-cell">{formatLastLogin(user.last_login_at, t)}</TableCell>
                  <TableCell className="hidden text-muted-foreground sm:table-cell">
                    {user.active_nodes} / {user.total_nodes}
                  </TableCell>
                  <TableCell>
                    <Badge variant={user.disabled ? 'secondary' : 'success'}>
                      {user.disabled ? t('users.status.disabled') : t('users.status.active')}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                    <Switch
                      checked={!user.disabled}
                      disabled={isSelf}
                      onCheckedChange={() => void handleToggleDisabled(user)}
                      aria-label={user.disabled ? t('users.actions.enable') : t('users.actions.disable')}
                      title={isSelf ? t('users.cannotDisableSelf') : undefined}
                    />
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}

      <CreateUserDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onCreated={(login) => {
          setDialogOpen(false)
          load()
          toast.success(t('users.created', { login }))
        }}
      />
    </div>
  )
}

function CreateUserDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (login: string) => void
}) {
  const { t } = useTranslation()
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [error, setError] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(false)
    setSubmitting(true)

    try {
      const user = await adminApi.createUser({ login, password, is_admin: isAdmin })
      setLogin('')
      setPassword('')
      setIsAdmin(false)
      onCreated(user.login)
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
          <DialogTitle>{t('users.createDialog.title')}</DialogTitle>
        </DialogHeader>
        <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="new-user-login">{t('common.login')}</Label>
            <Input id="new-user-login" type="text" value={login} onChange={(e) => setLogin(e.target.value)} required autoFocus />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="new-user-password">{t('common.password')}</Label>
            <PasswordInput
              id="new-user-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>
          <div className="flex items-center gap-2">
            <Switch id="new-user-is-admin" checked={isAdmin} onCheckedChange={setIsAdmin} />
            <Label htmlFor="new-user-is-admin" className="font-normal">
              {t('users.createDialog.promoteToAdmin')}
            </Label>
          </div>
          {isAdmin && <p className="text-xs text-muted-foreground">{t('users.createDialog.promoteToAdminHint')}</p>}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {t('users.errors.createFailed')}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={submitting}>
              {t('users.createDialog.submit')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
