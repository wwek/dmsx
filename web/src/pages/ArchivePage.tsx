import { useState } from 'react'
import { api } from '../api'
import type { ArchiveJob, ArchivePlan, Database, Storage } from '../api'
import { useI18n } from '../i18n'
import { act } from '../lib/format'

type Props = {
  database?: Database
  databases: Database[]
  storages: Storage[]
  plans: ArchivePlan[]
  archives: ArchiveJob[]
  refresh: () => Promise<void>
}

function mysqlFamily(databases: Database[]) {
  return databases.filter((db) => db.engine === 'mysql' || db.engine === 'mariadb')
}

function latestFailure(jobs: ArchiveJob[], planId: string) {
  const failed = jobs
    .filter((job) => job.planId === planId && job.status === 'failed' && job.error)
    .sort((a, b) => (b.startedAt || '').localeCompare(a.startedAt || ''))
  return failed[0]?.error || ''
}

export function ArchivePage({ database, databases, storages, plans, archives, refresh }: Props) {
  const { t, format, when } = useI18n()
  const [edit, setEdit] = useState<ArchivePlan | null>(null)
  const scope = database ? [database] : databases
  const targets = mysqlFamily(scope)
  plans = plans.filter((row) => !database || row.databaseId === database.id)
  archives = archives.filter((row) => !database || row.databaseId === database.id)
  const places = storages.filter((st) => st.type.toUpperCase() !== 'GOOGLE_DRIVE')

  return (
    <div className="split">
      <div>
        <form
          key={edit?.id || 'new-plan'}
          onSubmit={(event) => {
            event.preventDefault()
            const form = event.currentTarget
            const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
            act(async () => {
              const schedule: Record<string, unknown> = { type: data.scheduleType }
              if (data.scheduleType === 'CRON') {
                schedule.cronExpression = data.timeOfDay
              } else if (data.scheduleType !== 'HOURLY') {
                schedule.timeOfDay = data.timeOfDay
              }
              if (data.scheduleType === 'WEEKLY') {
                schedule.weekday = Number(data.weekday || edit?.schedule?.weekday || 1)
              }
              if (data.scheduleType === 'MONTHLY') {
                schedule.dayOfMonth = Number(data.dayOfMonth || edit?.schedule?.dayOfMonth || 1)
              }
              const body = {
                name: data.name,
                databaseId: data.databaseId,
                storageId: data.storageId,
                table: data.table,
                where: data.where,
                year: Number(data.year),
                batchSize: Number(data.batchSize),
                pauseMs: Number(data.pauseMs || 200),
                deleteAfter: (form.elements.namedItem('deleteAfter') as HTMLInputElement).checked,
                schedule,
                enabled: edit ? edit.enabled !== false : true,
              }
              if (edit?.id) {
                await api(`/api/archive/plans/${edit.id}`, { method: 'PUT', body: JSON.stringify(body) })
              } else {
                await api('/api/archive/plans', { method: 'POST', body: JSON.stringify(body) })
              }
              form.reset()
              setEdit(null)
              await refresh()
            })
          }}
        >
          <h2>{edit ? t.archive.editTitle : t.archive.planTitle}</h2>
          <p className="muted">{t.archive.mysqlOnly}</p>
          <p className="muted">{t.archive.driveHidden}</p>
          <label>
            {t.common.name}
            <input name="name" required placeholder="orders-2024" defaultValue={edit?.name || ''} />
          </label>
          {database ? (
            <input type="hidden" name="databaseId" value={database.id} />
          ) : (
            <label>
              {t.common.database}
              <select name="databaseId" defaultValue={edit?.databaseId || ''} disabled={!targets.length}>
                {targets.map((db) => (
                  <option key={db.id} value={db.id}>
                    {db.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label>
            {t.nav.storages}
            <select name="storageId" defaultValue={edit?.storageId || ''} disabled={!places.length}>
              {places.map((st) => (
                <option key={st.id} value={st.id}>
                  {st.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t.archive.table}
            <input name="table" required placeholder="orders" defaultValue={edit?.table || ''} />
          </label>
          <label>
            {t.archive.where}
            <input
              name="where"
              required
              defaultValue={edit?.where || "create_time < '2025-01-01'"}
            />
          </label>
          <label>
            {t.archive.year}
            <input name="year" type="number" defaultValue={edit?.year ?? 0} />
          </label>
          <p className="muted">{t.archive.yearHint}</p>
          <label>
            {t.archive.batchSize}
            <input name="batchSize" type="number" defaultValue={edit?.batchSize || 5000} />
          </label>
          <label>
            {t.archive.pauseMs}
            <input name="pauseMs" type="number" defaultValue={edit?.pauseMs || 200} />
          </label>
          <label>
            {t.archive.schedule}
            <select name="scheduleType" defaultValue={edit?.schedule?.type || 'DAILY'}>
              {['HOURLY', 'DAILY', 'WEEKLY', 'MONTHLY', 'CRON'].map((type) => (
                <option key={type}>{type}</option>
              ))}
            </select>
          </label>
          <label>
            {t.backups.timeOrCron}
            <input
              name="timeOfDay"
              defaultValue={edit?.schedule?.timeOfDay || edit?.schedule?.cronExpression || '03:00'}
            />
          </label>
          <label>
            {t.backups.weekday}
            <input name="weekday" type="number" min={0} max={7} defaultValue={edit?.schedule?.weekday ?? 1} />
          </label>
          <label>
            {t.backups.dayOfMonth}
            <input name="dayOfMonth" type="number" min={1} max={31} defaultValue={edit?.schedule?.dayOfMonth ?? 1} />
          </label>
          <label className="check">
            <input name="deleteAfter" type="checkbox" defaultChecked={!!edit?.deleteAfter} />
            <span>{t.archive.deleteAfter}</span>
          </label>
          <div className="actions">
            <button className="primary" type="submit" disabled={!targets.length || !places.length}>
              {edit ? t.common.saveChanges : t.archive.savePlan}
            </button>
            {edit ? (
              <button className="ghost" type="button" onClick={() => setEdit(null)}>
                {t.common.cancelEdit}
              </button>
            ) : null}
          </div>
        </form>

        <form
          style={{ marginTop: 22 }}
          onSubmit={(event) => {
            event.preventDefault()
            const form = event.currentTarget
            const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
            act(async () => {
              await api('/api/archive/jobs', {
                method: 'POST',
                body: JSON.stringify({
                  databaseId: data.databaseId,
                  storageId: data.storageId,
                  table: data.table,
                  where: data.where,
                  year: Number(data.year),
                  batchSize: Number(data.batchSize),
                  pauseMs: Number(data.pauseMs || 200),
                  deleteAfter: (form.elements.namedItem('deleteAfterOnce') as HTMLInputElement).checked,
                }),
              })
              form.reset()
              await refresh()
            })
          }}
        >
          <h2>{t.archive.title}</h2>
          {database ? (
            <input type="hidden" name="databaseId" value={database.id} />
          ) : (
            <label>
              {t.common.database}
              <select name="databaseId" disabled={!targets.length}>
                {targets.map((db) => (
                  <option key={db.id} value={db.id}>
                    {db.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label>
            {t.nav.storages}
            <select name="storageId" disabled={!places.length}>
              {places.map((st) => (
                <option key={st.id} value={st.id}>
                  {st.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t.archive.table}
            <input name="table" required placeholder="orders" />
          </label>
          <label>
            {t.archive.where}
            <input name="where" required defaultValue="create_time < '2025-01-01'" />
          </label>
          <label>
            {t.archive.year}
            <input name="year" type="number" defaultValue={new Date().getUTCFullYear()} />
          </label>
          <label>
            {t.archive.batchSize}
            <input name="batchSize" type="number" defaultValue={5000} />
          </label>
          <label>
            {t.archive.pauseMs}
            <input name="pauseMs" type="number" defaultValue={200} />
          </label>
          <label className="check">
            <input name="deleteAfterOnce" type="checkbox" />
            <span>{t.archive.deleteAfter}</span>
          </label>
          <button className="primary" type="submit" disabled={!targets.length || !places.length}>
            {t.archive.start}
          </button>
        </form>
      </div>

      <div>
        <h2>{t.archive.plans}</h2>
        <div className="list">
          {plans.map((row) => (
            <article key={row.id}>
              <div className="row">
                <strong>{row.name}</strong>
                <span className="tag">
                  {row.schedule?.type || '—'}
                  {row.enabled === false ? ` · ${t.backups.disabled}` : ''}
                </span>
              </div>
              <span className="muted">
                {databases.find((db) => db.id === row.databaseId)?.name || row.databaseId} ·{' '}
                {storages.find((st) => st.id === row.storageId)?.name || row.storageId} · {row.table} · {row.where}
              </span>
              {row.lastRunAt ? <span className="muted">{format(t.archive.lastRun, { time: when(row.lastRunAt) })}</span> : null}
              {latestFailure(archives, row.id) ? (
                <span className="err">{format(t.archive.lastFailure, { error: latestFailure(archives, row.id) })}</span>
              ) : null}
              <div className="actions">
                <button
                  className="primary"
                  type="button"
                  onClick={() =>
                    act(async () => {
                      await api(`/api/archive/plans/${row.id}/run`, { method: 'POST' })
                      await refresh()
                    })
                  }
                >
                  {t.archive.runNow}
                </button>
                <button
                  className="ghost"
                  type="button"
                  onClick={() =>
                    act(async () => {
                      await api(`/api/archive/plans/${row.id}`, {
                        method: 'PUT',
                        body: JSON.stringify({ ...row, enabled: row.enabled === false }),
                      })
                      await refresh()
                    })
                  }
                >
                  {row.enabled === false ? t.archive.enable : t.archive.disable}
                </button>
                <button className="ghost" type="button" onClick={() => setEdit(row)}>
                  {t.common.edit}
                </button>
                <button
                  className="danger"
                  type="button"
                  onClick={() =>
                    act(async () => {
                      await api(`/api/archive/plans/${row.id}`, { method: 'DELETE' })
                      if (edit?.id === row.id) setEdit(null)
                      await refresh()
                    })
                  }
                >
                  {t.common.delete}
                </button>
              </div>
            </article>
          ))}
          {!plans.length ? <p className="muted">{t.archive.emptyPlans}</p> : null}
        </div>

        <h2 style={{ marginTop: 22 }}>{t.archive.jobs}</h2>
        <div className="list">
          {archives.map((row) => (
            <article key={row.id}>
              <div className="row">
                <strong>
                  {row.table} · {row.year}
                </strong>
                <span className="tag">
                  {row.status}
                  {row.phase ? ` · ${row.phase}` : ''}
                </span>
              </div>
              <span className="muted">
                {when(row.startedAt)}
                {row.finishedAt ? ` · ${t.ddl.finished} ${when(row.finishedAt)}` : ''}
              </span>
              <span className="muted">{row.objectKey}</span>
              {(row.objectKeys?.length || 0) > 1 ? (
                <span className="muted">
                  {t.archive.objects}: {row.objectKeys?.join(', ')}
                </span>
              ) : null}
              {row.md5 ? (
                <span className="muted" title={row.md5}>
                  {t.archive.md5}: {row.md5}
                </span>
              ) : null}
              <span>
                {format(t.archive.summary, {
                  archived: row.rowsArchived || 0,
                  deleted: row.rowsDeleted || 0,
                })}
              </span>
              {row.error ? <span className="err">{row.error}</span> : null}
              {row.status === 'running' ? (
                <button
                  className="ghost"
                  type="button"
                  onClick={() =>
                    act(async () => {
                      await api(`/api/runs/${row.id}/cancel`, { method: 'POST' })
                      await refresh()
                    })
                  }
                >
                  {t.common.cancel}
                </button>
              ) : null}
            </article>
          ))}
          {!archives.length ? <p className="muted">{t.archive.empty}</p> : null}
        </div>
      </div>
    </div>
  )
}
