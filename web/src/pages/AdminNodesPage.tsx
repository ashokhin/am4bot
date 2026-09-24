import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { adminApi } from '../api/admin'
import type { AdminNodeView } from '../api/types'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'

/**
 * Admin-only, read-only: every node across every user -- see
 * requireNonAdminUser's doc comment on why an admin has none of their own
 * to create or edit here, only visibility into everyone else's.
 */
export function AdminNodesPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [nodes, setNodes] = useState<AdminNodeView[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [confirmUpdateOpen, setConfirmUpdateOpen] = useState(false)
  const [updating, setUpdating] = useState(false)

  async function handleUpdateAll() {
    setUpdating(true)

    try {
      const { queued, failed } = await adminApi.updateAllNodes()

      if (failed > 0) {
        toast.error(t('adminNodes.updateAll.partial', { queued, failed }))
      } else {
        toast.success(t('adminNodes.updateAll.queued', { count: queued }))
      }
    } catch {
      toast.error(t('adminNodes.updateAll.failed'))
    } finally {
      setUpdating(false)
    }
  }

  useEffect(() => {
    adminApi
      .listAllNodes()
      .then(setNodes)
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold">{t('adminNodes.title')}</h1>
        <Button variant="outline" disabled={updating} onClick={() => setConfirmUpdateOpen(true)} data-testid="update-all-nodes">
          {t('adminNodes.updateAll.button')}
        </Button>
      </div>

      {loading && <p className="text-muted-foreground">{t('common.loading')}</p>}
      {loadError && (
        <p role="alert" className="text-destructive">
          {t('adminNodes.errors.loadFailed')}
        </p>
      )}

      {!loading && !loadError && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('adminNodes.table.owner')}</TableHead>
              <TableHead>{t('adminNodes.table.name')}</TableHead>
              {/* Hidden below sm -- the row navigates to the node's own
                  detail page, which shows the full service list. */}
              <TableHead className="hidden sm:table-cell">{t('adminNodes.table.services')}</TableHead>
              <TableHead>{t('adminNodes.table.status')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {nodes.map((node) => (
              <TableRow key={node.id} className="cursor-pointer" onClick={() => navigate(`/admin/nodes/${node.id}`)}>
                <TableCell className="font-medium">{node.owner_login}</TableCell>
                <TableCell>
                  {node.name}
                  {node.is_default && (
                    <Badge variant="secondary" className="ml-2">
                      {t('nodes.defaultBadge')}
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="hidden max-w-72 truncate text-muted-foreground sm:table-cell">
                  {node.services.join(', ') || '—'}
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
      )}

      <ConfirmDialog
        open={confirmUpdateOpen}
        onOpenChange={setConfirmUpdateOpen}
        title={t('adminNodes.updateAll.button')}
        description={t('adminNodes.updateAll.confirm')}
        onConfirm={() => void handleUpdateAll()}
      />
    </div>
  )
}
