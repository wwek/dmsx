import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { ChangePassword } from '../components/ChangePassword'
import { NavIcon } from '../components/NavIcon'
import { Preferences } from '../components/Preferences'
import { SECTIONS } from '../constants/nav'
import type { Section } from '../constants/nav'
import { useI18n } from '../i18n'

type Props = {
  section: Section
  onSection: (section: Section) => void
  collapsed: boolean
  onToggleCollapsed: () => void
  onLogout: () => void
  onPasswordChanged: () => void
  version: string
  // error is the message of the last failed load. It is shown above the page,
  // so a failed load does not look like a machine without data.
  error?: string
  children: ReactNode
}

export function AppLayout({
  section,
  onSection,
  collapsed,
  onToggleCollapsed,
  onLogout,
  onPasswordChanged,
  version,
  error,
  children,
}: Props) {
  const { t } = useI18n()
  const title = t.nav[section]
  const [accountOpen, setAccountOpen] = useState(false)
  const accountRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!accountOpen) return
    function onPointer(event: PointerEvent) {
      if (!accountRef.current?.contains(event.target as Node)) setAccountOpen(false)
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') setAccountOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [accountOpen])

  return (
    <div className={`shell ${collapsed ? 'collapsed' : ''}`}>
      <aside>
        <div className="brand-row">
          <strong className="brand">{t.app.brand}</strong>
          <button className="ghost icon" type="button" onClick={onToggleCollapsed} title={t.app.collapse}>
            ☰
          </button>
        </div>
        <nav>
          {SECTIONS.map((id) => (
            <button
              key={id}
              type="button"
              className={section === id ? 'active' : ''}
              onClick={() => onSection(id)}
            >
              <NavIcon id={id} />
              <span>{t.nav[id]}</span>
            </button>
          ))}
        </nav>
        <footer className="aside-foot">
          <span className="version">{version}</span>
        </footer>
      </aside>
      <main>
        <header className="top">
          <p className="crumb">{title}</p>
          <div className="top-tools">
            <Preferences />
            <a
              className="ghost pref-trigger icon github"
              href="https://github.com/wwek/dmsx"
              target="_blank"
              rel="noreferrer"
              aria-label="GitHub"
              title="https://github.com/wwek/dmsx"
            >
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M12 2C6.48 2 2 6.58 2 12.26c0 4.52 2.87 8.35 6.84 9.7.5.1.68-.22.68-.48 0-.24-.01-.87-.01-1.7-2.78.62-3.37-1.37-3.37-1.37-.45-1.18-1.11-1.5-1.11-1.5-.91-.64.07-.63.07-.63 1 .07 1.53 1.06 1.53 1.06.9 1.57 2.36 1.12 2.94.86.09-.67.35-1.12.63-1.38-2.22-.26-4.55-1.14-4.55-5.07 0-1.12.39-2.03 1.03-2.75-.1-.26-.45-1.3.1-2.71 0 0 .84-.27 2.75 1.05a9.36 9.36 0 0 1 2.5-.34c.85 0 1.7.11 2.5.34 1.91-1.32 2.75-1.05 2.75-1.05.55 1.41.2 2.45.1 2.71.64.72 1.03 1.63 1.03 2.75 0 3.94-2.34 4.8-4.57 5.06.36.32.68.94.68 1.9 0 1.37-.01 2.48-.01 2.81 0 .27.18.59.69.48A10.04 10.04 0 0 0 22 12.26C22 6.58 17.52 2 12 2z" />
              </svg>
            </a>
            <div className="account" ref={accountRef}>
            <button
              className="avatar"
              type="button"
              aria-label={t.app.signedIn}
              aria-expanded={accountOpen}
              onClick={() => setAccountOpen((open) => !open)}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="8" r="3.2" />
                <path d="M5 19c1.4-3 3.8-4.5 7-4.5S17.6 16 19 19" />
              </svg>
            </button>
            {accountOpen ? (
              <div className="account-menu">
                <div className="account-head">
                  <strong>{t.app.signedIn}</strong>
                  <span className="muted">{t.app.localService}</span>
                </div>
                <ChangePassword onChanged={onPasswordChanged} />
                <button className="ghost" type="button" onClick={onLogout}>
                  {t.app.logout}
                </button>
              </div>
            ) : null}
            </div>
          </div>
        </header>
        <div className="view">
          {error ? (
            <p className="alert" role="alert">
              {t.app.loadFailed} {error}
            </p>
          ) : null}
          {children}
        </div>
      </main>
    </div>
  )
}
