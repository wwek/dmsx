import { useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useTheme } from '../hooks/useTheme'
import { useI18n } from '../i18n'

type Option<T extends string> = { id: T; label: string }

export function Preferences() {
  const { t, localeMode, setLocaleMode } = useI18n()
  const { mode, setMode } = useTheme()
  const [open, setOpen] = useState<'language' | 'appearance' | null>(null)

  return (
    <div className="prefs">
      <Choice
        label={t.app.language}
        value={localeMode}
        open={open === 'language'}
        onOpen={(next) => setOpen(next ? 'language' : null)}
        onChange={setLocaleMode}
        trigger={<Globe />}
        options={[
          { id: 'zh', label: '中文' },
          { id: 'en', label: 'English' },
          { id: 'system', label: t.app.followSystem },
        ]}
      />
      <Choice
        label={t.app.appearance}
        value={mode}
        open={open === 'appearance'}
        onOpen={(next) => setOpen(next ? 'appearance' : null)}
        onChange={setMode}
        icon
        options={[
          { id: 'light', label: t.app.themeLight },
          { id: 'dark', label: t.app.themeDark },
          { id: 'system', label: t.app.followSystem },
        ]}
      />
    </div>
  )
}

function Choice<T extends string>({
  label,
  value,
  options,
  open,
  onOpen,
  onChange,
  icon,
  trigger,
}: {
  label: string
  value: T
  options: Option<T>[]
  open: boolean
  onOpen: (open: boolean) => void
  onChange: (next: T) => void
  icon?: boolean
  trigger?: ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)
  const menuId = useId()
  const current = options.find((option) => option.id === value) || options[0]

  useEffect(() => {
    if (!open) return
    function onPointer(event: PointerEvent) {
      if (!ref.current?.contains(event.target as Node)) onOpen(false)
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') onOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open, onOpen])

  return (
    <div className="choice" ref={ref}>
      <button
        className={icon ? 'ghost pref-trigger icon' : 'ghost pref-trigger'}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={`${label}，${current.label}`}
        title={`${label}，${current.label}`}
        onClick={() => onOpen(!open)}
      >
        {trigger}
        {icon ? <Mark id={String(value)} /> : <span>{current.label}</span>}
      </button>
      {open ? (
        <div className="pref-menu" id={menuId} role="menu" aria-label={label}>
          <span>{label}</span>
          {options.map((option) => (
            <button
              key={option.id}
              type="button"
              role="menuitemradio"
              aria-checked={option.id === value}
              onClick={() => {
                onChange(option.id)
                onOpen(false)
              }}
            >
              {icon ? <Mark id={option.id} /> : <LangMark id={option.id} />}
              <span>{option.label}</span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function Globe() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="8" />
      <path d="M4 12h16" />
      <path d="M12 4c2.4 2.6 3.6 5.4 3.6 8s-1.2 5.4-3.6 8c-2.4-2.6-3.6-5.4-3.6-8s1.2-5.4 3.6-8z" />
    </svg>
  )
}

function LangMark({ id }: { id: string }) {
  if (id === 'system') return <Mark id="system" />
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <text x="12" y="16.5" textAnchor="middle">
        {id === 'zh' ? '中' : 'A'}
      </text>
    </svg>
  )
}

function Mark({ id }: { id: string }) {
  const paths: Record<string, ReactNode> = {
    light: (
      <>
        <circle cx="12" cy="12" r="3.2" />
        <path d="M12 3.5v2.2M12 18.3v2.2M4.8 4.8l1.6 1.6M17.6 17.6l1.6 1.6M3.5 12h2.2M18.3 12h2.2M4.8 19.2l1.6-1.6M17.6 6.4l1.6-1.6" />
      </>
    ),
    dark: <path d="M16 14.5A6.5 6.5 0 1 1 9.5 4 5.2 5.2 0 0 0 16 14.5z" />,
    system: (
      <>
        <rect x="4" y="5" width="16" height="11" rx="1.5" />
        <path d="M9 19.5h6M12 16v3.5" />
      </>
    ),
  }
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      {paths[id]}
    </svg>
  )
}
