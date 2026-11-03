import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import { getLanguage } from '@/lib/prefs'
import { en } from './en'
import { ru } from './ru'

i18n.use(initReactI18next).init({
  resources: { ru: { translation: ru }, en: { translation: en } },
  lng: getLanguage(),
  fallbackLng: 'ru',
  interpolation: { escapeValue: false }, // React escapes output itself
})

i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
})
document.documentElement.lang = i18n.language

export default i18n
