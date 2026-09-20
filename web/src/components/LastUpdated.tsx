import { useTranslation } from 'react-i18next'

/** "Updated HH:MM:SS" -- the last time this page's data was successfully
 * (re)fetched, see usePolling. null (before the first successful fetch)
 * renders nothing. */
export function LastUpdated({ at }: { at: Date | null }) {
  const { t } = useTranslation()

  if (!at) {
    return null
  }

  return <p className="text-xs text-muted-foreground">{t('metrics.updatedAt', { time: at.toLocaleTimeString() })}</p>
}
