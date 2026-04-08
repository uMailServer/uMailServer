import { Globe } from 'lucide-react'
import useI18n from '../hooks/useI18n'

const languages = [
  { code: 'en', name: 'English', flag: '🇺🇸' },
  { code: 'tr', name: 'Türkçe', flag: '🇹🇷' },
]

export default function LanguageSelector() {
  const { locale, changeLocale } = useI18n()

  return (
    <div className="relative inline-block">
      <select
        value={locale}
        onChange={(e) => changeLocale(e.target.value)}
        className="appearance-none bg-gray-800 text-white border border-gray-700 rounded px-3 py-1 pr-8 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
      >
        {languages.map((lang) => (
          <option key={lang.code} value={lang.code}>
            {lang.flag} {lang.name}
          </option>
        ))}
      </select>
      <Globe className="absolute right-2 top-1/2 transform -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none" />
    </div>
  )
}
