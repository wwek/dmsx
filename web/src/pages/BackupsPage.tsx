import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { api, downloadAuth, openTextAuth } from '../api'
import type { BackupConfig, BackupRun, Database, RestoreRun, Storage, WalSegment } from '../api'
import { useI18n } from '../i18n'
import { act, csv, formatBytes } from '../lib/format'

type Props = {
  database: Database
  storages: Storage[]
  configs: BackupConfig[]
  runs: BackupRun[]
  restores: RestoreRun[]
  walSegments: WalSegment[]
  pane: 'plan' | 'runs' | 'restore'
  refresh: () => Promise<void>
  onOpenStorages?: () => void
}

export function BackupsPage({
  database,
  storages,
  configs: allConfigs,
  runs: allRuns,
  restores: allRestores,
  walSegments: allWal,
  pane,
  refresh,
  onOpenStorages,
}: Props) {
  const configs = allConfigs.filter((row) => row.databaseId === database.id)
  const runs = allRuns.filter((row) => row.databaseId === database.id)
  const restores = allRestores.filter((row) => row.databaseId === database.id)
  const walSegments = allWal.filter((row) => row.databaseId === database.id)
  const { t, when, format } = useI18n()
  const [edit, setEdit] = useState<BackupConfig | null>(null)
  const gfs = edit?.retention?.gfs || { hours: 24, days: 7, weeks: 4, months: 6, years: 1 }
  const [method, setMethod] = useState(edit?.method || 'logical')
  const [restoreCodec, setRestoreCodec] = useState('')
  const configName = (id?: string) => configs.find((c) => c.id === id)?.name || id || ''
  const logicalRestores = runs.filter(
    (r) =>
      r.status === 'success' &&
      !!r.codec &&
      ['mysql-sql', 'mariadb-sql', 'pg-custom', 'mongo-archive'].includes(r.codec),
  )
  useEffect(() => {
    setEdit(null)
  }, [database.id])
  useEffect(() => {
    if (!logicalRestores.some((r) => r.codec === restoreCodec)) {
      setRestoreCodec(logicalRestores[0]?.codec || '')
    }
    // logicalRestores is derived from runs each render; key on run ids.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs, restoreCodec])
  const canPhysical = database.engine === 'postgresql'
  const showPitr =
    canPhysical &&
    (configs.some((c) => c.method === 'physical') || walSegments.length > 0)
  useEffect(() => {
    setMethod(edit?.method || 'logical')
  }, [edit?.id, edit?.method])
  useEffect(() => {
    if (!canPhysical && method === 'physical') setMethod('logical')
  }, [canPhysical, method])


  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
    const schedule: Record<string, unknown> = { type: data.scheduleType }
    if (data.scheduleType !== 'HOURLY' && data.scheduleType !== 'CRON') schedule.timeOfDay = data.timeOfDay
    if (data.scheduleType === 'WEEKLY') schedule.weekday = Number(data.weekday || 1)
    if (data.scheduleType === 'MONTHLY') schedule.dayOfMonth = Number(data.dayOfMonth || 1)
    if (data.scheduleType === 'CRON') schedule.cronExpression = data.timeOfDay
    const body = {
      name: data.name,
      databaseId: data.databaseId,
      storageId: data.storageId,
      method: data.method,
      physicalMode: data.method === 'physical' ? data.physicalMode || 'full' : '',
      physicalCodec: data.method === 'physical' ? data.physicalCodec || '' : '',
      walEnabled:
        data.method === 'physical' &&
        (form.elements.namedItem('walEnabled') as HTMLInputElement | null)?.checked === true,
      schedule,
      retention: {
        kind: data.retention,
        period: data.period || 'WEEK',
        count: Number(data.count || 7),
        gfs: {
          hours: Number(data.gfsHours || 24),
          days: Number(data.gfsDays || 7),
          weeks: Number(data.gfsWeeks || 4),
          months: Number(data.gfsMonths || 6),
          years: Number(data.gfsYears || 1),
        },
      },
      encrypted: (form.elements.namedItem('encrypted') as HTMLInputElement).checked,
      retry: (form.elements.namedItem('retry') as HTMLInputElement).checked,
      enabled: (form.elements.namedItem('enabled') as HTMLInputElement).checked,
      excludeTables: csv(data.excludeTables || ''),
      includeSchemas: csv(data.includeSchemas || ''),
      excludeTableData: csv(data.excludeTableData || ''),
      maxBackupSizeMb: Number(data.maxBackupSizeMb || 0),
      maxTotalSizeMb: Number(data.maxTotalSizeMb || 0),
      maxFailedTries: Number(data.maxFailedTries || 3),
    }
    await act(async () => {
      if (data.id) await api(`/api/backups/configs/${data.id}`, { method: 'PUT', body: JSON.stringify(body) })
      else await api('/api/backups/configs', { method: 'POST', body: JSON.stringify(body) })
      setEdit(null)
      await refresh()
    })
  }

  return (
    <div>
      {pane === 'plan' ? (
      <form key={edit?.id || 'new'} onSubmit={onSubmit}>
        <h2>{edit ? t.backups.editTitle : t.backups.createTitle}</h2>
        {!edit && !configs.length && !storages.length ? (
          <p className="guide">
            <span>{t.backups.needStorage}</span>
            {onOpenStorages ? (
              <button className="ghost" type="button" onClick={onOpenStorages}>
                {t.backups.goStorage}
              </button>
            ) : null}
          </p>
        ) : null}
        {!edit && !configs.length && storages.length ? <p className="muted">{t.backups.planHint}</p> : null}
        <input type="hidden" name="id" defaultValue={edit?.id || ''} />
        <label>
          {t.common.name}
          <input name="name" required defaultValue={edit?.name || ''} />
        </label>
        <input type="hidden" name="databaseId" value={database.id} />
        <label>
          {t.nav.storages}
          <select name="storageId" defaultValue={edit?.storageId || ''}>
            {storages.map((st) => (
              <option key={st.id} value={st.id}>
                {st.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t.backups.method}
          <select
            name="method"
            value={method}
            onChange={(e) => setMethod(e.target.value)}
          >
            <option value="logical">{t.backups.methodLogical}</option>
            <option value="physical" disabled={!canPhysical}>
              {t.backups.methodPhysical}
            </option>
          </select>
        </label>
        {!canPhysical ? <p className="muted">{t.backups.physicalPgOnly}</p> : null}
        {method === 'physical' && canPhysical ? (
          <>
            <label>
              {t.backups.physicalMode}
              <select name="physicalMode" defaultValue={edit?.physicalMode || 'full'}>
                <option value="full">{t.backups.physicalModeFull}</option>
                <option value="full_incr">{t.backups.physicalModeIncr}</option>
              </select>
            </label>
            <label>
              {t.backups.physicalCodec}
              <select name="physicalCodec" defaultValue={edit?.physicalCodec || ''}>
                <option value="">zstd</option>
                <option value="gzip">gzip</option>
                <option value="none">none</option>
              </select>
            </label>
            <label className="check">
              <input name="walEnabled" type="checkbox" defaultChecked={!!edit?.walEnabled} />
              <span>{t.backups.walEnabled}</span>
            </label>
          </>
        ) : null}
        <label>
          {t.backups.schedule}
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
            defaultValue={edit?.schedule?.timeOfDay || edit?.schedule?.cronExpression || '02:00'}
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
        <label>
          {t.backups.excludeTables}
          <input name="excludeTables" defaultValue={(edit?.excludeTables || []).join(', ')} />
        </label>
        <label>
          {t.backups.includeSchemas}
          <input name="includeSchemas" defaultValue={(edit?.includeSchemas || []).join(', ')} />
        </label>
        <label>
          {t.backups.excludeTableData}
          <input name="excludeTableData" defaultValue={(edit?.excludeTableData || []).join(', ')} />
        </label>
        <label>
          {t.backups.retention}
          <select name="retention" defaultValue={edit?.retention?.kind || 'COUNT'}>
            <option value="COUNT">{t.backups.retentionCount}</option>
            <option value="TIME_PERIOD">{t.backups.retentionPeriod}</option>
            <option value="GFS">{t.backups.retentionGfs}</option>
          </select>
        </label>
        <label>
          {t.backups.countOrDays}
          <input name="count" type="number" defaultValue={edit?.retention?.count || 7} />
        </label>
        <label>
          {t.backups.retentionWindow}
          <select name="period" defaultValue={edit?.retention?.period || 'WEEK'}>
            {['DAY', 'WEEK', 'MONTH', '3_MONTH', '6_MONTH', 'YEAR', '2_YEARS', '3_YEARS', '4_YEARS', '5_YEARS', 'FOREVER'].map(
              (item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ),
            )}
          </select>
        </label>
        <label>
          {t.backups.gfsHours}
          <input name="gfsHours" type="number" defaultValue={gfs.hours} />
        </label>
        <label>
          {t.backups.gfsDays}
          <input name="gfsDays" type="number" defaultValue={gfs.days} />
        </label>
        <label>
          {t.backups.gfsWeeks}
          <input name="gfsWeeks" type="number" defaultValue={gfs.weeks} />
        </label>
        <label>
          {t.backups.gfsMonths}
          <input name="gfsMonths" type="number" defaultValue={gfs.months} />
        </label>
        <label>
          {t.backups.gfsYears}
          <input name="gfsYears" type="number" defaultValue={gfs.years} />
        </label>
        <label>
          {t.backups.maxBackupMb}
          <input name="maxBackupSizeMb" type="number" defaultValue={edit?.maxBackupSizeMb || 0} />
        </label>
        <label>
          {t.backups.maxTotalMb}
          <input name="maxTotalSizeMb" type="number" defaultValue={edit?.maxTotalSizeMb || 0} />
        </label>
        <label>
          {t.backups.maxFailedTries}
          <input name="maxFailedTries" type="number" defaultValue={edit?.maxFailedTries || 3} />
        </label>
        <label className="check">
          <input name="encrypted" type="checkbox" defaultChecked={!!edit?.encrypted} />
          <span>{t.backups.encrypted}</span>
        </label>
        <label className="check">
          <input name="retry" type="checkbox" defaultChecked={!edit || edit.retry} />
          <span>{t.backups.retry}</span>
        </label>
        <label className="check">
          <input name="enabled" type="checkbox" defaultChecked={!edit || edit.enabled} />
          <span>{t.backups.enabled}</span>
        </label>
        <div className="actions">
          <button className="primary" type="submit">
            {edit ? t.common.saveChanges : t.backups.savePlan}
          </button>
          {edit ? (
            <button className="ghost" type="button" onClick={() => setEdit(null)}>
              {t.common.cancelEdit}
            </button>
          ) : null}
        </div>
      </form>
      ) : null}
      {pane === 'plan' ? (
      <div>
        <div className="list">
          {configs.map((row) => (
            <article key={row.id}>
              <div className="row">
                <strong>{row.name}</strong>
                <span className="tag">
                  {row.method}
                  {row.method === 'physical' && row.physicalMode ? `/${row.physicalMode}` : ''}
                  {row.enabled ? '' : ` · ${t.backups.disabled}`}
                </span>
              </div>
              <span className="muted">
                {row.schedule?.type} · {t.backups.retention} {row.retention?.kind}
                {row.lastRunAt ? ` · ${when(row.lastRunAt)}` : ''}
                {row.failCount ? ` · ${format(t.overview.failHint, { n: row.failCount })}` : ''}
              </span>
              <div className="actions">
                <button
                  className="primary"
                  type="button"
                  onClick={() =>
                    act(async () => {
                      await api(`/api/backups/configs/${row.id}/run`, {
                        method: 'POST',
                        body: JSON.stringify({ forceFull: false }),
                      })
                      await refresh()
                    })
                  }
                >
                  {t.backups.runNow}
                </button>
                {row.method === 'physical' && row.physicalMode === 'full_incr' ? (
                  <button
                    className="ghost"
                    type="button"
                    onClick={() =>
                      act(async () => {
                        await api(`/api/backups/configs/${row.id}/run`, {
                          method: 'POST',
                          body: JSON.stringify({ forceFull: true }),
                        })
                        await refresh()
                      })
                    }
                  >
                    {t.backups.runFull}
                  </button>
                ) : null}
                <button
                  className="ghost"
                  type="button"
                  onClick={() =>
                    act(async () => {
                      await api(`/api/backups/configs/${row.id}`, {
                        method: 'PUT',
                        body: JSON.stringify({ ...row, enabled: !row.enabled }),
                      })
                      await refresh()
                    })
                  }
                >
                  {row.enabled ? t.backups.disable : t.backups.enable}
                </button>
                <button className="ghost" type="button" onClick={() => setEdit(row)}>
                  {t.common.edit}
                </button>
                <button
                  className="danger"
                  type="button"
                  onClick={() =>
                    act(async () => {
                      await api(`/api/backups/configs/${row.id}`, { method: 'DELETE' })
                      setEdit(null)
                      await refresh()
                    })
                  }
                >
                  {t.common.delete}
                </button>
              </div>
            </article>
          ))}
          {!configs.length ? <p className="muted">{t.backups.empty}</p> : null}
        </div>
      </div>
      ) : null}
      {pane === 'runs' ? (
      <div>
        <h2>{t.backups.runs}</h2>
        <div className="list">
          {runs.map((row) => (
            <article key={row.id}>
              <div className="row">
                <strong>
                  {row.status}
                  {configName(row.configId) ? ` · ${configName(row.configId)}` : ''}
                </strong>
                <span className="muted">{when(row.startedAt)}</span>
              </div>
              <span>
                {row.fileName}{' '}
                {row.status === 'running'
                  ? `· ${format(t.backups.runningBytes, { size: formatBytes(row.bytes || 0) })}`
                  : row.bytes
                    ? `· ${formatBytes(row.bytes)}`
                    : ''}
                {row.encrypted ? ` · ${t.backups.encryptedTag}` : ''}
                {row.forceFull ? ' · FULL' : ''}
              </span>
              {row.finishedAt ? (
                <span className="muted">
                  {t.backups.finishedAt}: {when(row.finishedAt)}
                </span>
              ) : null}
              {row.sha256 ? (
                <span className="muted" title={row.sha256}>
                  {t.backups.checksum}: {row.sha256.slice(0, 12)}…
                </span>
              ) : null}
              {row.physicalKind || row.startLsn || row.parentRunId || row.timelineId || row.rootRunId || row.manifestKey ? (
                <span className="muted">
                  {[
                    row.physicalKind,
                    row.startLsn && row.stopLsn ? `${row.startLsn} → ${row.stopLsn}` : '',
                    row.timelineId ? `tl ${row.timelineId}` : '',
                    row.systemId ? `sys ${row.systemId}` : '',
                    row.parentRunId ? `parent ${row.parentRunId.slice(0, 8)}` : '',
                    row.rootRunId ? `root ${row.rootRunId.slice(0, 8)}` : '',
                    row.manifestKey ? 'manifest' : '',
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              ) : null}
              {row.error ? <span className="err">{row.error}</span> : null}
              <div className="actions">
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
                {row.status === 'success' ? (
                  <button
                    className="ghost"
                    type="button"
                    onClick={() =>
                      act(() => downloadAuth(`/api/backups/runs/${row.id}/download`, row.fileName || 'backup'))
                    }
                  >
                    {t.backups.download}
                  </button>
                ) : null}
                {row.status !== 'running' ? (
                  <button
                    className="danger"
                    type="button"
                    onClick={() =>
                      act(async () => {
                        await api(`/api/backups/runs/${row.id}`, { method: 'DELETE' })
                        await refresh()
                      })
                    }
                  >
                    {t.common.delete}
                  </button>
                ) : null}
              </div>
            </article>
          ))}
          {!runs.length ? <p className="muted">{t.backups.runsEmpty}</p> : null}
        </div>
        <h2 style={{ marginTop: 22 }}>{t.backups.walSegments}</h2>
        <div className="actions" style={{ marginBottom: 10 }}>
          {showPitr ? (
            <button
              className="ghost"
              type="button"
              onClick={() => act(() => openTextAuth(`/api/wal/pitr-guide?databaseId=${database.id}`))}
            >
              {t.backups.pitrGuide}: {database.name}
            </button>
          ) : null}
        </div>
        <div className="list">
          {walSegments.map((row) => (
            <article key={row.id}>
              <div className="row">
                <strong>{row.name}</strong>
                <span className="muted">{when(row.uploadedAt)}</span>
              </div>
              <span className="muted">
                {row.objectKey} · {formatBytes(row.bytes)}
                {row.timelineId ? ` · tl ${row.timelineId}` : ''}
              </span>
            </article>
          ))}
          {!walSegments.length ? <p className="muted">{t.backups.walEmpty}</p> : null}
        </div>
      </div>
      ) : null}
      {pane === 'restore' ? (
      <div>
        <h2>{t.backups.restores}</h2>
        <form
          className="card"
          onSubmit={(event) => {
            event.preventDefault()
            const form = event.currentTarget
            const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
            act(async () => {
              await api(`/api/backups/runs/${data.backupId}/restore`, {
                method: 'POST',
                body: JSON.stringify({
                  host: data.host,
                  port: Number(data.port),
                  username: data.username,
                  password: data.password,
                  database: data.database,
                  ssl: (form.elements.namedItem('ssl') as HTMLInputElement | null)?.checked === true,
                  parallel: restoreCodec === 'pg-custom' ? Number(data.parallel || 0) : 0,
                  verify: (form.elements.namedItem('verify') as HTMLInputElement | null)?.checked === true,
                }),
              })
              await refresh()
            })
          }}
        >
          <p className="muted">{t.backups.restoreLogicalOnly}</p>
          {!logicalRestores.length ? <p className="muted">{t.backups.noLogicalBackup}</p> : null}
          <label>
            {t.backups.backup}
            <select
              name="backupId"
              required
              disabled={!logicalRestores.length}
              onChange={(e) => {
                const hit = logicalRestores.find((r) => r.id === e.target.value)
                setRestoreCodec(hit?.codec || '')
              }}
            >
              {logicalRestores.map((r) => (
                <option key={r.id} value={r.id}>
                  {when(r.startedAt)} · {r.codec}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t.common.host}
            <input name="host" required defaultValue="127.0.0.1" />
          </label>
          <label>
            {t.common.port}
            <input name="port" type="number" required defaultValue={3306} />
          </label>
          <label>
            {t.common.user}
            <input name="username" required />
          </label>
          <label>
            {t.gate.password}
            <input name="password" type="password" />
          </label>
          <label>
            {t.backups.dbName}
            <input name="database" required />
          </label>
          {restoreCodec === 'pg-custom' ? (
            <label>
              {t.backups.parallel}
              <input name="parallel" type="number" defaultValue={0} min={0} max={32} />
            </label>
          ) : null}
          <label className="check">
            <input name="ssl" type="checkbox" />
            <span>{t.backups.restoreSsl}</span>
          </label>
          <label className="check">
            <input name="verify" type="checkbox" defaultChecked />
            <span>{t.backups.verify}</span>
          </label>
          <button className="primary" type="submit" disabled={!logicalRestores.length}>
            {t.backups.startRestore}
          </button>
        </form>
        <div className="list" style={{ marginTop: 12 }}>
          {restores.map((row) => (
            <article key={row.id}>
              <div className="row">
                <strong>{row.database}</strong>
                <span className="tag">{row.status}</span>
              </div>
              <span className="muted">{when(row.startedAt)}</span>
              {row.report ? <span className="muted">{row.report}</span> : null}
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
          {!restores.length ? <p className="muted">{t.backups.restoresEmpty}</p> : null}
        </div>
      </div>
      ) : null}
    </div>
  )
}