import type { BackupConfig, BackupRun, Database, HealthCheck, Storage } from '../api'
import type { Section } from '../constants/nav'
import { HealthBadge } from '../components/HealthBadge'
import { useI18n } from '../i18n'
import { formatBytes } from '../lib/format'

type Props = {
  databases: Database[]
  storages: Storage[]
  configs: BackupConfig[]
  runs: BackupRun[]
  healthOf: (id: string) => HealthCheck
  onSection: (section: Section) => void
}

export function OverviewPage({ databases, storages, configs, runs, healthOf, onSection }: Props) {
  const { t, format, when, locale } = useI18n()
  const down = databases.filter((db) => healthOf(db.id).status === 'UNAVAILABLE')
  const nameSep = locale === 'zh' ? '、' : ', '
  const recent = [...runs]
    .sort((a, b) => String(b.startedAt || '').localeCompare(String(a.startedAt || '')))
    .slice(0, 8)
  const steps = [
    { done: databases.length > 0, label: t.overview.setupDatabase, section: 'databases' as const },
    { done: storages.length > 0, label: t.overview.setupStorage, section: 'storages' as const },
    { done: configs.length > 0, label: t.overview.setupPlan, section: 'databases' as const },
  ]
  const ready = steps.every((step) => step.done)

  return (
    <>
      {down.length ? (
        <p className="alert">
          {format(t.overview.alert, { n: down.length, names: down.map((db) => db.name).join(nameSep) })}
        </p>
      ) : null}
      {ready ? (
        <h1>{format(t.overview.readyTitle, { n: databases.length })}</h1>
      ) : (
        <>
          <h1>{t.overview.setupTitle}</h1>
          <p className="muted">{t.overview.setupBody}</p>
          <div className="setup">
            {steps.map((step) => (
              <button key={step.label} type="button" className={step.done ? 'done' : ''} onClick={() => onSection(step.section)}>
                <span className="mark" aria-hidden="true">
                  {step.done ? '✓' : '·'}
                </span>
                <span>{step.label}</span>
              </button>
            ))}
          </div>
        </>
      )}
      {databases.length ? (
        <div className="list">
          {databases.map((db) => {
            const health = healthOf(db.id)
            return (
              <article key={db.id}>
                <div className="row">
                  <strong>{db.name}</strong>
                  <HealthBadge status={health.status} attempts={health.attempts} showError />
                </div>
                <span className="muted">{format(t.health.lastCheck, { time: when(health.checkedAt) })}</span>
                {(() => {
                  const err = (health.attempts || []).find((a) => a.error)?.error
                  return err ? <span className="err">{format(t.health.lastError, { error: err })}</span> : null
                })()}
              </article>
            )
          })}
        </div>
      ) : null}
      <h2>{t.overview.recentRuns}</h2>
      <div className="list">
        {recent.map((row) => (
          <article key={row.id}>
            <div className="row">
              <strong>{row.status}</strong>
              <span className="muted">{when(row.startedAt)}</span>
            </div>
            <span className="muted">
              {row.fileName || row.codec || row.id}
              {row.status === 'running'
                ? ` · ${format(t.backups.runningBytes, { size: formatBytes(row.bytes || 0) })}`
                : row.bytes
                  ? ` · ${formatBytes(row.bytes)}`
                  : ''}
              {row.physicalKind ? ` · ${row.physicalKind}` : ''}
            </span>
            {row.error ? <span className="err">{row.error}</span> : null}
          </article>
        ))}
        {!recent.length ? <p className="muted">{t.overview.recentEmpty}</p> : null}
      </div>
    </>
  )
}
