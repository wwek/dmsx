import { useEffect, useState } from 'react'

const STORAGE_KEY = 'dmsx.theme'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

export function readThemeMode(): ThemeMode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'light' || saved === 'dark' || saved === 'system') return saved
  } catch {
    /* private mode */
  }
  return 'system'
}

export function systemTheme(): ResolvedTheme {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  return mode === 'system' ? systemTheme() : mode
}

export function useTheme() {
  const [mode, setMode] = useState<ThemeMode>(readThemeMode)

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const apply = () => {
      document.documentElement.setAttribute('data-theme', resolveTheme(mode))
    }
    apply()
    try {
      localStorage.setItem(STORAGE_KEY, mode)
    } catch {
      /* private mode */
    }
    if (mode !== 'system') return
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [mode])

  const resolved = resolveTheme(mode)
  return {
    mode,
    setMode,
    resolved,
    theme: resolved,
    toggleTheme: () => setMode(resolved === 'dark' ? 'light' : 'dark'),
  }
}
