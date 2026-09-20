import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'
import en from './locales/en.json'
import ru from './locales/ru.json'

// English is the default/fallback for every user and every string that a
// locale hasn't translated yet; Russian is the one other supported
// language for now. Add a new locale by adding it to `resources` and
// `supportedLngs` below -- nothing else needs to change.
export const supportedLngs = ['en', 'ru'] as const
export type SupportedLng = (typeof supportedLngs)[number]

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      ru: { translation: ru },
    },
    fallbackLng: 'en',
    supportedLngs: [...supportedLngs],
    interpolation: {
      escapeValue: false, // React already escapes rendered output
    },
    detection: {
      // remember an explicit choice (see LanguageSwitcher) across visits,
      // otherwise fall back to the browser's own language list
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
    },
  })

export default i18n
