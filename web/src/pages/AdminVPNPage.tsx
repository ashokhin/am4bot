import { PlusIcon } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { vpnRegionsApi } from '../api/vpnRegions'
import type { VPNProviderStatus, VPNRegion } from '../api/types'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { PasswordInput } from '../components/ui/password-input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'
import { Textarea } from '../components/ui/textarea'

/**
 * Admin-only: curates the shared VPN region catalog and the one provider
 * account every region connects through -- see internal/store/vpn_regions.go's
 * doc comment. There's no per-user or per-node VPN config here on purpose.
 */
export function AdminVPNPage() {
  const { t } = useTranslation()
  const [regions, setRegions] = useState<VPNRegion[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [providerStatus, setProviderStatus] = useState<VPNProviderStatus | undefined>(undefined)

  const [regionFormOpen, setRegionFormOpen] = useState(false)
  const [regionName, setRegionName] = useState('')
  const [ovpnConfig, setOvpnConfig] = useState('')
  const [regionError, setRegionError] = useState<string | undefined>(undefined)
  const [regionSubmitting, setRegionSubmitting] = useState(false)
  const [pendingDeleteRegion, setPendingDeleteRegion] = useState<VPNRegion | undefined>(undefined)

  const [providerName, setProviderName] = useState('')
  const [vpnUsername, setVpnUsername] = useState('')
  const [vpnPassword, setVpnPassword] = useState('')
  const [providerError, setProviderError] = useState<string | undefined>(undefined)
  const [providerSubmitting, setProviderSubmitting] = useState(false)

  function load() {
    Promise.all([vpnRegionsApi.list(), vpnRegionsApi.getProviderStatus()])
      .then(([r, p]) => {
        setRegions(r)
        setProviderStatus(p)
      })
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  async function handleCreateRegion(e: FormEvent) {
    e.preventDefault()
    setRegionError(undefined)
    setRegionSubmitting(true)

    try {
      await vpnRegionsApi.create({ name: regionName, ovpn_config: ovpnConfig })
      setRegionName('')
      setOvpnConfig('')
      setRegionFormOpen(false)
      load()
      toast.success(t('vpnAdmin.regionCreated', { name: regionName }))
    } catch {
      setRegionError(t('vpnAdmin.errors.createRegionFailed'))
    } finally {
      setRegionSubmitting(false)
    }
  }

  async function handleDeleteRegion(region: VPNRegion) {
    try {
      await vpnRegionsApi.remove(region.id)
      setRegions((prev) => prev.filter((r) => r.id !== region.id))
      toast.success(t('vpnAdmin.regionDeleted', { name: region.name }))
    } catch {
      toast.error(t('vpnAdmin.errors.deleteRegionFailed'))
    }
  }

  async function handleSetProvider(e: FormEvent) {
    e.preventDefault()
    setProviderError(undefined)
    setProviderSubmitting(true)

    try {
      const status = await vpnRegionsApi.setProviderCredentials({
        provider: providerName || undefined,
        vpn_username: vpnUsername,
        vpn_password: vpnPassword,
      })
      setProviderStatus(status)
      setVpnUsername('')
      setVpnPassword('')
      toast.success(t('vpnAdmin.providerSaved'))
    } catch {
      setProviderError(t('vpnAdmin.errors.setProviderFailed'))
    } finally {
      setProviderSubmitting(false)
    }
  }

  if (loading) {
    return <p className="text-muted-foreground">{t('common.loading')}</p>
  }
  if (loadError) {
    return (
      <p role="alert" className="text-destructive">
        {t('vpnAdmin.errors.loadFailed')}
      </p>
    )
  }

  return (
    <div className="flex max-w-6xl flex-col gap-6">
      <h1 className="text-2xl font-semibold">{t('vpnAdmin.title')}</h1>

      <Card>
        <CardHeader className="flex flex-col items-start gap-2 sm:flex-row sm:items-center sm:justify-between">
          <CardTitle className="text-base">{t('vpnAdmin.providerSection')}</CardTitle>
          <Badge variant={providerStatus?.configured ? 'success' : 'secondary'} className="whitespace-normal text-left">
            {providerStatus?.configured
              ? t('vpnAdmin.providerConfigured', { provider: providerStatus.provider })
              : t('vpnAdmin.providerNotConfigured')}
          </Badge>
        </CardHeader>
        <CardContent>
          <form onSubmit={(e) => void handleSetProvider(e)} className="flex max-w-sm flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="provider">{t('vpnAdmin.form.provider')}</Label>
              <Input
                id="provider"
                type="text"
                name="provider"
                value={providerName}
                onChange={(e) => setProviderName(e.target.value)}
                placeholder="custom"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="vpn-username">{t('vpnAdmin.form.vpnUsername')}</Label>
              <Input
                id="vpn-username"
                type="text"
                name="vpn_username"
                value={vpnUsername}
                onChange={(e) => setVpnUsername(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="vpn-password">{t('vpnAdmin.form.vpnPassword')}</Label>
              <PasswordInput
                id="vpn-password"
                name="vpn_password"
                value={vpnPassword}
                onChange={(e) => setVpnPassword(e.target.value)}
                required
              />
            </div>
            {providerError && (
              <p role="alert" className="text-sm text-destructive">
                {providerError}
              </p>
            )}
            <Button type="submit" disabled={providerSubmitting} className="w-fit">
              {t('common.save')}
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base">{t('vpnAdmin.regionsSection')}</CardTitle>
          <Button variant="outline" size="sm" onClick={() => setRegionFormOpen((v) => !v)}>
            <PlusIcon /> {t('vpnAdmin.addRegion')}
          </Button>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {regionFormOpen && (
            <form onSubmit={(e) => void handleCreateRegion(e)} className="flex max-w-md flex-col gap-4 rounded-md border p-4">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="region-name">{t('vpnAdmin.form.regionName')}</Label>
                <Input
                  id="region-name"
                  type="text"
                  name="region-name"
                  value={regionName}
                  onChange={(e) => setRegionName(e.target.value)}
                  required
                  autoFocus
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="ovpn-config">{t('vpnAdmin.form.ovpnConfig')}</Label>
                <Textarea
                  id="ovpn-config"
                  name="ovpn-config"
                  value={ovpnConfig}
                  onChange={(e) => setOvpnConfig(e.target.value)}
                  required
                  rows={6}
                  className="font-mono text-xs"
                />
              </div>
              {regionError && (
                <p role="alert" className="text-sm text-destructive">
                  {regionError}
                </p>
              )}
              <Button type="submit" disabled={regionSubmitting} className="w-fit">
                {t('common.save')}
              </Button>
            </form>
          )}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('vpnAdmin.table.name')}</TableHead>
                <TableHead className="text-right">{t('vpnAdmin.table.actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {regions.map((r) => (
                <TableRow key={r.id}>
                  <TableCell className="font-medium">{r.name}</TableCell>
                  <TableCell className="text-right">
                    <Button variant="ghost" size="sm" onClick={() => setPendingDeleteRegion(r)}>
                      {t('vpnAdmin.actions.delete')}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={pendingDeleteRegion !== undefined}
        onOpenChange={(open) => !open && setPendingDeleteRegion(undefined)}
        title={t('vpnAdmin.actions.delete')}
        description={pendingDeleteRegion ? t('vpnAdmin.confirmDeleteRegion', { name: pendingDeleteRegion.name }) : ''}
        destructive
        onConfirm={() => pendingDeleteRegion && void handleDeleteRegion(pendingDeleteRegion)}
      />
    </div>
  )
}
