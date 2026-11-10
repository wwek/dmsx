import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import en from './locales/en'
import zh from './locales/zh'
import type { Locale, Messages } from './types'

const catalogs: Record<Locale, Messages> = { zh, en }

const STORAGE_KEY = 'dmsx.locale'

export type LocaleMode = Locale | 'system'

type I18nValue = {
  locale: Locale
  localeMode: LocaleMode
  setLocaleMode: (mode: LocaleMode) => void
  t: Messages
  format: (template: string, vars?: Record<string, string | number>) => string
  when: (value?: string) => string
}

const I18nContext = createContext<I18nValue | null>(null)

export function readLocaleMode(): LocaleMode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'zh' || saved === 'en' || saved === 'system') return saved
  } catch {
    /* private mode */
  }
  return 'system'
}

export function systemLocale(): Locale {
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

function resolveLocale(mode: LocaleMode): Locale {
  return mode === 'system' ? systemLocale() : mode
}

function fill(template: string, vars?: Record<string, string | number>) {
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (_, key: string) => String(vars[key] ?? ''))
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [localeMode, setLocaleModeState] = useState<LocaleMode>(readLocaleMode)
  const [locale, setLocale] = useState<Locale>(() => resolveLocale(readLocaleMode()))

  const setLocaleMode = useCallback((next: LocaleMode) => {
    setLocaleModeState(next)
    setLocale(resolveLocale(next))
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      /* private mode */
    }
  }, [])

  useEffect(() => {
    document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en'
  }, [locale])

  useEffect(() => {
    if (localeMode !== 'system') return
    const onChange = () => setLocale(systemLocale())
    window.addEventListener('languagechange', onChange)
    return () => window.removeEventListener('languagechange', onChange)
  }, [localeMode])

  const value = useMemo<I18nValue>(() => {
    const messages = catalogs[locale]
    const dateLocale = locale === 'zh' ? 'zh-CN' : 'en-US'
    return {
      locale,
      localeMode,
      setLocaleMode,
      t: messages,
      format: fill,
      when: (value?: string) => {
        if (!value || value.startsWith('0001-')) return messages.common.noneYet
        return new Date(value).toLocaleString(dateLocale, { hour12: false })
      },
    }
  }, [locale, localeMode, setLocaleMode])

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n() {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used within I18nProvider')
  return ctx
}

export type { Locale, Messages }
