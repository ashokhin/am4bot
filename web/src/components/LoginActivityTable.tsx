import { useTranslation } from 'react-i18next'
import type { LoginActivityEntry } from '../api/types'
import { Badge } from './ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table'

/**
 * The last loginActivityLimit (10, see internal/api/login_activity.go)
 * login attempts, success and failure both -- used both on a user's own
 * Settings page (their own activity) and the admin user detail page
 * (someone else's).
 */
export function LoginActivityTable({ entries }: { entries: LoginActivityEntry[] }) {
  const { t } = useTranslation()

  if (entries.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('loginActivity.empty')}</p>
  }

  return (
    <>
      {/* Below sm: cards, not a table -- the User-Agent column is a
          100+ character string that makes horizontal table scrolling
          (even with a frozen first column) impractical on a phone.
          Wrapping it across a few lines in a card is the only layout
          that stays readable without side-scrolling for it. */}
      <div className="flex flex-col gap-3 sm:hidden">
        {entries.map((e, i) => (
          <div key={`${e.created_at}-${i}`} className="rounded-md border p-3 text-sm">
            <div className="flex items-center justify-between gap-2">
              <span className="whitespace-nowrap">{new Date(e.created_at).toLocaleString()}</span>
              <Badge variant={e.success ? 'success' : 'destructive'}>
                {e.success ? t('loginActivity.success') : t('loginActivity.failure')}
              </Badge>
            </div>
            <div className="mt-1 font-mono text-xs text-muted-foreground">{e.ip}</div>
            <div className="mt-1 text-xs break-all text-muted-foreground">{e.user_agent}</div>
          </div>
        ))}
      </div>

      <Table className="hidden sm:table">
        <TableHeader>
          <TableRow>
            <TableHead>{t('loginActivity.time')}</TableHead>
            <TableHead>{t('loginActivity.result')}</TableHead>
            <TableHead>{t('loginActivity.ip')}</TableHead>
            <TableHead>{t('loginActivity.userAgent')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {entries.map((e, i) => (
            // No stable id in the API response -- index + timestamp is
            // fine, this list is never reordered/filtered in place.
            <TableRow key={`${e.created_at}-${i}`}>
              <TableCell className="whitespace-nowrap text-sm">{new Date(e.created_at).toLocaleString()}</TableCell>
              <TableCell>
                <Badge variant={e.success ? 'success' : 'destructive'}>
                  {e.success ? t('loginActivity.success') : t('loginActivity.failure')}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-xs">{e.ip}</TableCell>
              <TableCell className="max-w-xs truncate text-xs text-muted-foreground" title={e.user_agent}>
                {e.user_agent}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </>
  )
}
