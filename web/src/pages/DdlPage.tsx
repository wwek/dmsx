import { useState } from 'react'
import { api } from '../api'
import type { Database, DdlRun } from '../api'
import { useI18n } from '../i18n'
import { act } from '../lib/format'

type Props = {
  database?: Database
  databases: Database[]
  ddls: DdlRun[]
  refresh: () => Promise<void>
}

export function DdlPage({ database, databases, ddls, refresh }: Props) {
  const { t, when } = useI18n()
  const scope = database ? [database] : databases
  const targets = scope.filter((db) => db.engine === 'mysql' || db.engine === 'mariadb')
  ddls = ddls.filter((row) => !database || row.databaseId === database.id)
  const [ddlLog, setDdlLog] = useState('')
  const [ddlMeta, setDdlMeta] = useState('')
  const [ddlPercent, setDdlPercent] = useState(0)

  function watch(row: DdlRun) {
    setDdlLog(row.log || '')
    setDdlPercent(row.percent || 0)
    setDdlMeta(
      `${row.tool || 'gh-ost'} · ${row.execute ? t.ddl.cutover : t.ddl.dryRun}` +
        (row.error ? ` · ${row.error}` : row.status ? ` · ${row.status}` : ''),
    )
    if (row.status && row.status !== 'running') return
    const socket = new WebSocket(`${location.origin.replace(/^http/, 'ws')}/api/ddl/runs/${row.id}/stream`)
    socket.onmessage = (event) => {
      const message = JSON.parse(event.data)
      if (message.line) setDdlLog((prev) => prev + message.line + '\n')
      if (message.kind === 'progress') {
        setDdlPercent(message.progress?.percent || 0)
        setDdlMeta(
          `${message.progress?.copied || 0} / ${message.progress?.total || '?'} · ETA ${message.progress?.eta || '—'}`,
        )
      }
      if (message.kind === 'done') {
        setDdlMeta(message.status + (message.error ? ' · ' + message.error : ''))
        refresh()
      }
    }
  }

  return (
    <div className="split">
      <form
        onSubmit={(event) => {
          event.preventDefault()
          const form = event.currentTarget
          const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
          act(async () => {
            const run = await api<{ id: string }>('/api/ddl/runs', {
              method: 'POST',
              body: JSON.stringify({
                databaseId: data.databaseId,
                statement: data.statement,
                tool: data.tool,
                chunkSize: Number(data.chunkSize),
                execute: (form.elements.namedItem('execute') as HTMLInputElement).checked,
              }),
            })
            watch({ id: run.id, status: 'running', tool: data.tool, execute: (form.elements.namedItem('execute') as HTMLInputElement).checked })
            await refresh()
          })
        }}
      >
        <h2>{t.ddl.title}</h2>
        <p className="muted">{t.ddl.mysqlOnly}</p>
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
          {t.ddl.statement}
          <textarea name="statement" required defaultValue="ALTER TABLE orders ADD COLUMN note varchar(64)" />
        </label>
        <label>
          {t.ddl.tool}
          <select name="tool">
            <option value="gh-ost">gh-ost</option>
            <option value="pt-osc">pt-online-schema-change</option>
          </select>
        </label>
        <label>
          {t.ddl.chunk}
          <input name="chunkSize" type="number" defaultValue={1000} />
        </label>
        <label className="check">
          <input name="execute" type="checkbox" />
          <span>{t.ddl.execute}</span>
        </label>
        <button className="primary" type="submit" disabled={!targets.length}>
          {t.ddl.start}
        </button>
      </form>
      <div>
        <article className="card">
          <strong>{t.ddl.shadowHint}</strong>
          <div className="bar" style={{ marginTop: 14 }}>
            <span style={{ width: `${ddlPercent}%` }} />
          </div>
          <p className="muted">{ddlMeta}</p>
          <pre className="log">{ddlLog}</pre>
        </article>
        <div className="list" style={{ marginTop: 12 }}>
          {ddls.map((row) => (
            <article key={row.id}>
              <div className="row">
                <strong>{row.table || t.ddl.change}</strong>
                <span className="tag">{row.status}</span>
              </div>
              <span className="muted">{row.statement}</span>
              <span className="muted">
                {row.tool || 'gh-ost'} · {row.execute ? t.ddl.cutover : t.ddl.dryRun}
                {row.chunkSize ? ` · ${t.ddl.chunk} ${row.chunkSize}` : ''}
              </span>
              <span className="muted">
                {when(row.startedAt)}
                {row.finishedAt ? ` · ${t.ddl.finished} ${when(row.finishedAt)}` : ''}
                {row.status === 'running' || row.percent
                  ? ` · ${Math.round(row.percent || 0)}% · ${row.copied || 0}/${row.total || '?'} · ETA ${row.eta || '—'}`
                  : ''}
              </span>
              {row.error ? <span className="err">{row.error}</span> : null}
              <div className="actions">
                <button className="ghost" type="button" onClick={() => watch(row)}>
                  {t.ddl.watch}
                </button>
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
              </div>
            </article>
          ))}
          {!ddls.length ? <p className="muted">{t.ddl.empty}</p> : null}
        </div>
      </div>
    </div>
  )
}
