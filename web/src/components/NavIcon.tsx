import type { ReactNode } from 'react'
import type { Section } from '../constants/nav'

type Props = { id: Section }

export function NavIcon({ id }: Props) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      {paths[id]}
    </svg>
  )
}

const paths: Record<Section, ReactNode> = {
  overview: (
    <>
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </>
  ),
  databases: (
    <>
      <ellipse cx="12" cy="6" rx="7" ry="3" />
      <path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6" />
      <path d="M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6" />
    </>
  ),
  storages: (
    <>
      <path d="M4 7h16v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7z" />
      <path d="M8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <path d="M4 12h16" />
    </>
  ),
  notifiers: (
    <>
      <path d="M6 9a6 6 0 0 1 12 0c0 7 2 7 2 9H4c0-2 2-2 2-9" />
      <path d="M10 20a2 2 0 0 0 4 0" />
    </>
  ),
  audit: (
    <>
      <path d="M8 4h8a2 2 0 0 1 2 2v14l-3-2-3 2-3-2-3 2V6a2 2 0 0 1 2-2z" />
      <path d="M9 9h6M9 13h6" />
    </>
  ),
}
