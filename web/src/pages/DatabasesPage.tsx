import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { api } from '../api'
import type {
  ArchiveJob,
  ArchivePlan,
  BackupConfig,
  BackupRun,
  Database,
  DdlRun,
  HealthCheck,
  RestoreRun,
  Storage,
  WalSegment,
} from '../api'
import type { Section } from '../constants/nav'
import { HealthBadge } from '../components/HealthBadge'
import { useI18n } from '../i18n'
import { act } from '../lib/format'
import { ArchivePage } from './ArchivePage'
import { BackupsPage } from './BackupsPage'
import { ColdQueryPage } from './ColdQueryPage'
import { DdlPage } from './DdlPage'

type Props = {
  databases: Database[]
  storages: Storage[]
  configs: BackupConfig[]
  runs: BackupRun[]
  restores: RestoreRun[]
  walSegments: WalSegment[]
  archivePlans: ArchivePlan[]
  archives: ArchiveJob[]
  ddls: DdlRun[]
  healthOf: (id: string) => HealthCheck
  refresh: () => Promise<void>
  onSection: (section: Section) => void
}

type DatabaseTab = 'connection' | 'backup' | 'runs' | 'restore' | 'archive' | 'cold' | 'ddl'

export function DatabasesPage({
  databases,
  storages,
  configs,
  runs,
  restores,
  walSegments,
  archivePlans,
  archives,
  ddls,
  healthOf,
  refresh,
  onSection,
}: Props) {
  const { t, format } = useI18n()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [tab, setTab] = useState<DatabaseTab>('connection')
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null)
  const edit = databases.find((row) => row.id === selectedId) || null
  const [engine, setEngine] = useState(edit?.engine || 'mysql')
  useEffect(() => {
    setEngine(edit?.engine || 'mysql')
    setNotice(null)
  }, [edit?.id, edit?.engine])
  const mysqlFamily = engine === 'mysql' || engine === 'mariadb'
  const lastFail = (id: string) => (healthOf(id).attempts || []).find((a) => a.error)?.error || ''


  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
    const body: Record<string, unknown> = {
      name: data.name,
      engine: data.engine,
      host: data.host,
      port: Number(data.port),
      username: data.username,
      password: data.password,
      database: data.database,
      version: data.version,
      ssl: engine === 'mongodb' ? false : (form.elements.namedItem('ssl') as HTMLInputElement | null)?.checked === true,
      includeTriggers: mysqlFamily && (form.elements.namedItem('includeTriggers') as HTMLInputElement | null)?.checked === true,
      includeEvents: mysqlFamily && (form.elements.namedItem('includeEvents') as HTMLInputElement | null)?.checked === true,
    }
    if (data.sshHost) {
      body.ssh = {
        host: data.sshHost,
        port: Number(data.sshPort || 22),
        user: data.sshUser,
        password: data.sshPassword,
        key: data.sshKey,
      }
    }
    setNotice(null)
    try {
      if (data.id) {
        await api(`/api/databases/${data.id}`, { method: 'PUT', body: JSON.stringify(body) })
        setNotice({ ok: true, text: t.common.saved })
      } else {
        const created = await api<Database>('/api/databases', { method: 'POST', body: JSON.stringify(body) })
        setSelectedId(created.id)
        setTab('backup')
      }
      await refresh()
    } catch (err) {
      setNotice({ ok: false, text: err instanceof Error ? err.message : String(err) })
    }
  }

  useEffect(() => {
    if (selectedId && databases.some((row) => row.id === selectedId)) setCreating(false)
  }, [selectedId, databases])

  const showForm = creating || !!edit || databases.length === 0

  return (
    <div className="catalog">
      <div className="list">
        <button
          className="primary"
          type="button"
          onClick={() => {
            setCreating(true)
            setSelectedId(null)
            setTab('connection')
            setNotice(null)
          }}
        >
          {t.databases.add}
        </button>
        {databases.map((row) => {
          const health = healthOf(row.id)
          return (
            <article key={row.id} className={row.id === selectedId && !creating ? 'selected' : ''}>
              <button
                className="card-open"
                type="button"
                onClick={() => {
                  setSelectedId(row.id)
                  setCreating(false)
                  setTab('connection')
                }}
              >
                <span className="row">
                  <strong>{row.name}</strong>
                  <span className="tag">{row.engine}</span>
                </span>
                <span className="muted">
                  {row.username}@{row.host}:{row.port}/{row.database}
                </span>
                <HealthBadge status={health.status} attempts={health.attempts} />
              </button>
            </article>
          )
        })}
        {!databases.length ? <p className="muted">{t.databases.empty}</p> : null}
      </div>
      <div>
        {!showForm ? <p className="muted">{t.databases.pick}</p> : null}
        {edit && !creating ? (
          <div className="tabs" role="tablist">
            {(['connection', 'backup', 'runs', 'restore', 'archive', 'cold', 'ddl'] as const).map((id) => (
              <button
                key={id}
                className="ghost"
                type="button"
                role="tab"
                aria-selected={tab === id}
                onClick={() => setTab(id)}
              >
                {t.databases.tabs[id]}
              </button>
            ))}
          </div>
        ) : null}
        {showForm && (creating || tab === 'connection') ? (
      <form key={edit?.id || 'new'} onSubmit={onSubmit}>
        <h2>{edit ? t.databases.editTitle : t.databases.createTitle}</h2>
        <input type="hidden" name="id" defaultValue={edit?.id || ''} />
        <label>
          {t.common.name}
          <input name="name" required defaultValue={edit?.name || ''} />
        </label>
        <label>
          {t.databases.engine}
          <select name="engine" value={engine} onChange={(e) => setEngine(e.target.value)}>
            {['mysql', 'mariadb', 'postgresql', 'mongodb'].map((item) => (
              <option key={item}>{item}</option>
            ))}
          </select>
        </label>
        <label>
          {t.common.host}
          <input name="host" required defaultValue={edit?.host || '127.0.0.1'} />
        </label>
        <label>
          {t.common.port}
          <input name="port" type="number" required defaultValue={edit?.port || 3306} />
        </label>
        <label>
          {t.common.user}
          <input name="username" required defaultValue={edit?.username || ''} />
        </label>
        <label>
          {t.common.password}
          <input name="password" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
        </label>
        <label>
          {t.common.database}
          <input name="database" required defaultValue={edit?.database || ''} />
        </label>
        <label>
          {t.databases.version}
          <input name="version" defaultValue={edit?.version || ''} />
        </label>
        {engine !== 'mongodb' ? (
          <label className="check">
            <input name="ssl" type="checkbox" defaultChecked={!!edit?.ssl} />
            <span>{t.databases.requireTls}</span>
          </label>
        ) : (
          <p className="muted">{t.databases.mongoSslHint}</p>
        )}
        {mysqlFamily ? (
          <>
            <label className="check">
              <input name="includeTriggers" type="checkbox" defaultChecked={!!edit?.includeTriggers} />
              <span>{t.databases.includeTriggers}</span>
            </label>
            <label className="check">
              <input name="includeEvents" type="checkbox" defaultChecked={!!edit?.includeEvents} />
              <span>{t.databases.includeEvents}</span>
            </label>
          </>
        ) : null}
        <h2>{t.databases.sshOptional}</h2>
        <label>
          {t.common.host}
          <input name="sshHost" defaultValue={edit?.ssh?.host || ''} />
        </label>
        <label>
          {t.common.port}
          <input name="sshPort" type="number" defaultValue={edit?.ssh?.port || 22} />
        </label>
        <label>
          {t.common.user}
          <input name="sshUser" defaultValue={edit?.ssh?.user || ''} />
        </label>
        <label>
          {t.common.password}
          <input name="sshPassword" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
        </label>
        <label>
          {t.databases.sshKey}
          <textarea name="sshKey" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
        </label>
        {notice ? <p className={notice.ok ? 'notice ok' : 'notice bad'}>{notice.text}</p> : null}
        <div className="actions">
          {edit ? (
            <button
              className="primary"
              type="button"
              onClick={async () => {
                setNotice(null)
                try {
                  await api(`/api/databases/${edit.id}/test`, { method: 'POST' })
                  setNotice({ ok: true, text: t.databases.testOk })
                } catch (err) {
                  setNotice({ ok: false, text: err instanceof Error ? err.message : String(err) })
                }
              }}
            >
              {t.databases.testConnection}
            </button>
          ) : null}
          <button className={edit ? 'ghost' : 'primary'} type="submit">
            {edit ? t.common.saveChanges : t.common.save}
          </button>
          {creating ? (
            <button
              className="ghost"
              type="button"
              onClick={() => {
                setCreating(false)
                setNotice(null)
              }}
            >
              {t.common.cancelEdit}
            </button>
          ) : null}
        </div>
      </form>
        ) : null}
      {edit && !creating && tab === 'connection' ? (
        <form
          style={{ marginTop: 16 }}
          onSubmit={(event) => {
            event.preventDefault()
            const form = event.currentTarget
            const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
            act(async () => {
              await api(`/api/databases/${edit.id}/healthcheck`, {
                method: 'PUT',
                body: JSON.stringify({
                  enabled: (form.elements.namedItem('enabled') as HTMLInputElement).checked,
                  notify: (form.elements.namedItem('notify') as HTMLInputElement).checked,
                  intervalMinutes: Number(data.intervalMinutes || 1),
                  failuresBeforeDown: Number(data.failuresBeforeDown || 3),
                }),
              })
              await refresh()
            })
          }}
        >
          <h2>{t.databases.healthTitle}</h2>
          <label className="check">
            <input
              name="enabled"
              type="checkbox"
              defaultChecked={edit.healthCheck?.enabled !== false}
            />
            <span>{t.databases.healthEnabled}</span>
          </label>
          <label className="check">
            <input name="notify" type="checkbox" defaultChecked={edit.healthCheck?.notify !== false} />
            <span>{t.databases.healthNotify}</span>
          </label>
          <label>
            {t.databases.healthInterval}
            <input
              name="intervalMinutes"
              type="number"
              min={1}
              defaultValue={edit.healthCheck?.intervalMinutes || 1}
            />
          </label>
          <label>
            {t.databases.healthFailures}
            <input
              name="failuresBeforeDown"
              type="number"
              min={1}
              defaultValue={edit.healthCheck?.failuresBeforeDown || 3}
            />
          </label>
          <button className="primary" type="submit">
            {t.databases.healthSave}
          </button>
        </form>
      ) : null}
        {edit && !creating && tab === 'connection' ? (
          <div className="actions" style={{ marginTop: 12 }}>
            <button
              className="ghost"
              type="button"
              onClick={() =>
                act(async () => {
                  const updated = await api<Database>(`/api/databases/${edit.id}/backup-user`, {
                    method: 'POST',
                    body: JSON.stringify({ replication: false }),
                  })
                  alert(format(t.databases.backupUserDone, { user: updated.username }))
                  await refresh()
                })
              }
            >
              {t.databases.backupUser}
            </button>
            {edit.engine === 'postgresql' ? (
              <button
                className="ghost"
                type="button"
                onClick={() =>
                  act(async () => {
                    const updated = await api<Database>(`/api/databases/${edit.id}/backup-user`, {
                      method: 'POST',
                      body: JSON.stringify({ replication: true }),
                    })
                    alert(format(t.databases.backupUserDone, { user: updated.username }))
                    await refresh()
                  })
                }
              >
                {t.databases.backupUserReplication}
              </button>
            ) : null}
            <button
              className="danger"
              type="button"
              onClick={() =>
                act(async () => {
                  await api(`/api/databases/${edit.id}`, { method: 'DELETE' })
                  setSelectedId(null)
                  await refresh()
                })
              }
            >
              {t.common.delete}
            </button>
          </div>
        ) : null}
        {edit && !creating && tab === 'connection' && lastFail(edit.id) ? (
          <p className="err">{format(t.health.lastError, { error: lastFail(edit.id) })}</p>
        ) : null}
        {edit && !creating && (tab === 'backup' || tab === 'runs' || tab === 'restore') ? (
          <BackupsPage
            key={edit.id + tab}
            database={edit}
            storages={storages}
            configs={configs}
            runs={runs}
            restores={restores}
            walSegments={walSegments}
            pane={tab === 'backup' ? 'plan' : tab}
            refresh={refresh}
            onOpenStorages={() => onSection('storages')}
          />
        ) : null}
        {edit && !creating && tab === 'archive' ? (
          <ArchivePage
            key={edit.id}
            database={edit}
            databases={databases}
            storages={storages}
            plans={archivePlans}
            archives={archives}
            refresh={refresh}
          />
        ) : null}
        {edit && !creating && tab === 'cold' ? (
          <ColdQueryPage
            key={edit.id}
            database={edit}
            databases={databases}
            storages={storages}
            archives={archives}
          />
        ) : null}
        {edit && !creating && tab === 'ddl' ? (
          <DdlPage key={edit.id} database={edit} databases={databases} ddls={ddls} refresh={refresh} />
        ) : null}
      </div>
    </div>
  )
}
