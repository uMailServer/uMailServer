import { createContext, useContext, useEffect, useState } from "react"

type Theme = "dark" | "light" | "system"

type ThemeProviderProps = {
  children: React.ReactNode
  defaultTheme?: Theme
  storageKey?: string
}

type ThemeProviderState = {
  theme: Theme
  setTheme: (theme: Theme) => void
  resolvedTheme: "dark" | "light"
}

const ThemeProviderContext = createContext<ThemeProviderState | null>(null)

export function ThemeProvider({
  children,
  defaultTheme = "system",
  storageKey = "vite-ui-theme",
  ...props
}: ThemeProviderProps) {
  const [theme, setTheme] = useState<Theme>(() => {
    // Persisted values come from outside the app's control (older versions,
    // other apps on the same origin — the default key here is shared by stock
    // Vite/shadcn apps): only union members may become the theme, anything
    // else falls back to the default.
    const stored = localStorage.getItem(storageKey)
    return stored === "dark" || stored === "light" || stored === "system"
      ? stored
      : defaultTheme
  })
  const [resolvedTheme, setResolvedTheme] = useState<"dark" | "light">("light")

  useEffect(() => {
    const root = window.document.documentElement
    const media = window.matchMedia("(prefers-color-scheme: dark)")

    const apply = () => {
      root.classList.remove("light", "dark")

      let resolved: "dark" | "light"
      if (theme === "system") {
        resolved = media.matches ? "dark" : "light"
      } else {
        resolved = theme
      }

      root.classList.add(resolved)
      setResolvedTheme(resolved)
    }

    apply()

    // 'system' means tracking the OS scheme: re-resolve when it changes.
    if (theme === "system") {
      media.addEventListener("change", apply)
      return () => media.removeEventListener("change", apply)
    }
  }, [theme])

  const value = {
    theme,
    setTheme: (theme: Theme) => {
      localStorage.setItem(storageKey, theme)
      setTheme(theme)
    },
    resolvedTheme,
  }

  return (
    <ThemeProviderContext.Provider {...props} value={value}>
      {children}
    </ThemeProviderContext.Provider>
  )
}

export const useTheme = () => {
  const context = useContext(ThemeProviderContext)

  if (!context)
    throw new Error("useTheme must be used within a ThemeProvider")

  return context
}
