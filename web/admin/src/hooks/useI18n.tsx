import { useState, useEffect, useCallback } from 'react'

type TranslationMessages = Record<string, unknown>

// Load translations
const translations: Record<string, () => Promise<{ default: TranslationMessages }>> = {
  en: () => import('../locales/en.json') as Promise<{ default: TranslationMessages }>,
  tr: () => import('../locales/tr.json') as Promise<{ default: TranslationMessages }>,
}

const STORAGE_KEY = 'umailserver-language'

export function useI18n() {
  const [locale, setLocale] = useState(() => {
    return localStorage.getItem(STORAGE_KEY) || navigator.language.split('-')[0] || 'en'
  })
  const [messages, setMessages] = useState<TranslationMessages | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const loadTranslations = async () => {
      setLoading(true)
      try {
        const loader = translations[locale] || translations.en
        const msgs = await loader()
        setMessages((msgs.default || msgs) as TranslationMessages)
      } catch (err) {
        console.error('Failed to load translations:', err)
        const fallback = await translations.en()
        setMessages((fallback.default || fallback) as TranslationMessages)
      }
      setLoading(false)
    }

    loadTranslations()
  }, [locale])

  const changeLocale = useCallback((newLocale: string) => {
    setLocale(newLocale)
    localStorage.setItem(STORAGE_KEY, newLocale)
    document.documentElement.lang = newLocale
  }, [])

  const t = useCallback(
    (key: string, params: Record<string, string> = {}): string => {
      if (!messages) return key

      const keys = key.split('.')
      let value: unknown = messages

      for (const k of keys) {
        value = (value as Record<string, unknown>)?.[k]
        if (value === undefined) return key
      }

      if (typeof value === 'string') {
        return value.replace(/\{\{(\w+)\}\}/g, (match, param) => {
          return params[param] !== undefined ? params[param] : match
        })
      }

      return key
    },
    [messages]
  )

  return { locale, changeLocale, t, loading, supportedLocales: Object.keys(translations) }
}

export default useI18n
