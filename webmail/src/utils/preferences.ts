// Webmail preferences kept in this browser only (the server has no settings
// endpoint for them). Only settings that something reads live here.
const SETTINGS_KEY = "umail.webmail.settings"

export const DEFAULT_PREFERENCES = {
  autoSaveDraft: true,
  spellCheck: true,
}
export type Preferences = typeof DEFAULT_PREFERENCES

export function readPreferences(): Preferences {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY)
    const parsed = raw ? JSON.parse(raw) : null
    const out = { ...DEFAULT_PREFERENCES }
    if (parsed && typeof parsed === "object") {
      for (const key of Object.keys(out) as (keyof Preferences)[]) {
        if (typeof parsed[key] === "boolean") out[key] = parsed[key]
      }
    }
    return out
  } catch {
    return { ...DEFAULT_PREFERENCES }
  }
}

// writePreferences throws when the browser refuses to store them.
export function writePreferences(prefs: Preferences): void {
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(prefs))
}
