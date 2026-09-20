import { useTranslation } from 'react-i18next'
import { supportedLngs } from '../i18n'

const labels: Record<string, string> = {
  en: 'English',
  ru: 'Русский',
}

export function LanguageSwitcher() {
  const { i18n, t } = useTranslation()

  return (
    <label className="flex items-center gap-1">
      <span className="sr-only">{t('common.language')}</span>
      <select
        value={i18n.resolvedLanguage}
        onChange={(e) => {
          void i18n.changeLanguage(e.target.value)
        }}
        className="h-8 rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
      >
        {supportedLngs.map((lng) => (
          <option key={lng} value={lng}>
            {labels[lng]}
          </option>
        ))}
      </select>
    </label>
  )
}
