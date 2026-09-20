import { PlusIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { toast } from 'sonner'
import { ApiError } from '../api/client'
import { nodesApi } from '../api/nodes'
import type { Node } from '../api/types'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Switch } from '../components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'

export function NodesPage() {
  const { t } = useTranslation()
  const [nodes, setNodes] = useState<Node[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Node | undefined>(undefined)

  useEffect(() => {
    nodesApi
      .list()
      .then(setNodes)
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }, [])

  async function handleToggleEnabled(node: Node) {
    try {
      const updated = await nodesApi.update(node.id, { enabled: !node.enabled })
      setNodes((prev) => prev.map((n) => (n.id === node.id ? updated : n)))
    } catch (err) {
      // Enabling a node that's missing credentials/a schedule gets a
      // specific, actionable 400 from the server (store.ErrNodeNotReady)
      // -- show that instead of a generic failure message.
      if (err instanceof ApiError && err.status === 400) {
        toast.error(err.message)
      } else {
        toast.error(t('nodes.errors.updateFailed'))
      }
    }
  }

  async function handleDelete(node: Node) {
    try {
      await nodesApi.remove(node.id)
      setNodes((prev) => prev.filter((n) => n.id !== node.id))
      toast.success(t('nodes.deleted', { name: node.name }))
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.error(t('nodes.errors.defaultNotDeletable'))
      } else {
        toast.error(t('nodes.errors.deleteFailed'))
      }
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{t('nodes.title')}</h1>
        <Button asChild>
          <Link to="/nodes/new">
            <PlusIcon /> {t('nodes.addNode')}
          </Link>
        </Button>
      </div>

      {loading && <p className="text-muted-foreground">{t('common.loading')}</p>}
      {loadError && (
        <p role="alert" className="text-destructive">
          {t('nodes.errors.loadFailed')}
        </p>
      )}

      {!loading && !loadError && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('nodes.table.name')}</TableHead>
              {/* Hidden below sm -- the node's own edit page shows the
                  full service list; not worth a horizontal scroll for
                  it in a summary table on a phone. */}
              <TableHead className="hidden sm:table-cell">{t('nodes.table.services')}</TableHead>
              <TableHead>{t('nodes.table.status')}</TableHead>
              <TableHead className="text-right">{t('nodes.table.actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {nodes.map((node) => (
              <TableRow key={node.id}>
                <TableCell>
                  <Link to={`/nodes/${node.id}`} className="font-medium text-primary hover:underline">
                    {node.name}
                  </Link>
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
                <TableCell>
                  <div className="flex items-center justify-end gap-3">
                    <Switch
                      checked={node.enabled}
                      onCheckedChange={() => void handleToggleEnabled(node)}
                      aria-label={node.enabled ? t('nodes.actions.disable') : t('nodes.actions.enable')}
                    />
                    {!node.is_default && (
                      <Button variant="ghost" size="sm" onClick={() => setPendingDelete(node)}>
                        {t('nodes.actions.delete')}
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <ConfirmDialog
        open={pendingDelete !== undefined}
        onOpenChange={(open) => !open && setPendingDelete(undefined)}
        title={t('nodes.actions.delete')}
        description={pendingDelete ? t('nodes.confirmDelete', { name: pendingDelete.name }) : ''}
        destructive
        onConfirm={() => pendingDelete && void handleDelete(pendingDelete)}
      />
    </div>
  )
}
