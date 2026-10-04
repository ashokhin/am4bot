import { ArrowLeftIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Button } from './ui/button'

// Goes to a fixed parent page rather than history.back(): a detail page
// opened straight from a link or bookmark has no in-app history to go back
// to, and back would leave the app entirely.
export function BackButton({ to }: { to: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <Button variant="ghost" size="sm" className="-ml-2 w-fit" onClick={() => navigate(to)}>
      <ArrowLeftIcon />
      {t('common.back')}
    </Button>
  )
}
