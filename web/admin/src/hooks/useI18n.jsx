import { useState, useEffect, useCallback } from 'react';

// Load translations
const translations = {
  en: () => import('../locales/en.json'),
  tr: () => import('../locales/tr.json'),
};

const STORAGE_KEY = 'umailserver-language';

export function useI18n() {
  const [locale, setLocale] = useState(() => {
    return localStorage.getItem(STORAGE_KEY) || navigator.language.split('-')[0] || 'en';
  });
  const [messages, setMessages] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const loadTranslations = async () => {
      setLoading(true);
      try {
        const loader = translations[locale] || translations.en;
        const msgs = await loader();
        setMessages(msgs.default || msgs);
      } catch (err) {
        console.error('Failed to load translations:', err);
        const fallback = await translations.en();
        setMessages(fallback.default || fallback);
      }
      setLoading(false);
    };

    loadTranslations();
  }, [locale]);

  const changeLocale = useCallback((newLocale) => {
    setLocale(newLocale);
    localStorage.setItem(STORAGE_KEY, newLocale);
    document.documentElement.lang = newLocale;
  }, []);

  const t = useCallback(
    (key, params = {}) => {
      if (!messages) return key;

      const keys = key.split('.');
      let value = messages;

      for (const k of keys) {
        value = value?.[k];
        if (value === undefined) return key;
      }

      if (typeof value === 'string') {
        return value.replace(/\{\{(\w+)\}\}/g, (match, param) => {
          return params[param] !== undefined ? params[param] : match;
        });
      }

      return key;
    },
    [messages]
  );

  return { locale, changeLocale, t, loading, supportedLocales: Object.keys(translations) };
}

export default useI18n;
