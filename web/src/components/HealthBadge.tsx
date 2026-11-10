import type { HealthAttempt } from '../api'
import { useI18n } from '../i18n'

type Props = {
  status: string
  attempts?: HealthAttempt[]
  showError?: boolean
}

export function HealthBadge({ status, attempts, showError }: Props) {
  const { t, format } = useI18n()
  if (status === 'AVAILABLE') return <span className="badge ok">{t.health.available}</span>
  if (status === 'UNAVAILABLE') {
    return (
      <span className="badge bad" title={lastError(attempts)}>
        {t.health.unavailable}
      </span>
    )
  }
  const fails = (attempts || []).filter((a) => a.status === 'UNAVAILABLE')
  if (fails.length) {
    return (
      <span className="badge" title={fails[0]?.error || undefined}>
        {format(t.health.probing, { n: fails.length })}
      </span>
    )
  }
  if (showError && lastError(attempts)) {
    return <span className="badge">{lastError(attempts)}</span>
  }
  return <span className="badge">{t.health.unknown}</span>
}

function lastError(attempts?: HealthAttempt[]) {
  const hit = (attempts || []).find((a) => a.error)
  return hit?.error || ''
}
