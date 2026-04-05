import { useState, useEffect, useCallback } from 'react';

interface Messages {
  [key: string]: string | Messages;
}

interface I18nReturn {
  locale: string;
  changeLocale: (locale: string) => void;
  t: (key: string, params?: Record<string, string | number>) => string;
  loading: boolean;
  supportedLocales: string[];
}

// Load translations
const translations: Record<string, () => Promise<{ default: Messages }>> = {
  en: () => import('../locales/en.json'),
  tr: () => import('../locales/tr.json'),
};

const STORAGE_KEY = 'umailserver-account-language';

export function useI18n(): I18nReturn {
  const [locale, setLocale] = useState<string>(() => {
    return localStorage.getItem(STORAGE_KEY) || navigator.language.split('-')[0] || 'en';
  });
  const [messages, setMessages] = useState<Messages | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const loadTranslations = async () => {
      setLoading(true);
      try {
        const loader = translations[locale] || translations.en;
        const msgs = await loader();
        setMessages(msgs.default);
      } catch (err) {
        console.error('Failed to load translations:', err);
        const fallback = await translations.en();
        setMessages(fallback.default);
      }
      setLoading(false);
    };

    loadTranslations();
  }, [locale]);

  const changeLocale = useCallback((newLocale: string) => {
    setLocale(newLocale);
    localStorage.setItem(STORAGE_KEY, newLocale);
    document.documentElement.lang = newLocale;
  }, []);

  const t = useCallback(
    (key: string, params: Record<string, string | number> = {}): string => {
      if (!messages) return key;

      const keys = key.split('.');
      let value: unknown = messages;

      for (const k of keys) {
        if (value && typeof value === 'object') {
          value = (value as Messages)[k];
        } else {
          return key;
        }
      }

      if (typeof value === 'string') {
        return value.replace(/\{\{(\w+)\}\}/g, (match, param) => {
          return params[param] !== undefined ? String(params[param]) : match;
        });
      }

      return key;
    },
    [messages]
  );

  return { locale, changeLocale, t, loading, supportedLocales: Object.keys(translations) };
}

export default useI18n;
