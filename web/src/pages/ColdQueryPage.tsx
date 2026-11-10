import { useState } from 'react'
import { api } from '../api'
import type { ArchiveJob, Database, Storage } from '../api'
import { COLD_QUERY_TYPES } from '../constants/nav'
import { useI18n } from '../i18n'

type Props = {
  database?: Database
  databases: Database[]
  storages: Storage[]
  archives: ArchiveJob[]
}

export function ColdQueryPage({ database, databases, storages, archives }: Props) {
  const { t } = useI18n()
  const [coldRows, setColdRows] = useState<Record<string, unknown>[] | null>(null)
  const scope = database ? [database] : databases
  const targets = scope.filter((db) => db.engine === 'mysql' || db.engine === 'mariadb')
  const places = storages.filter((st) => COLD_QUERY_TYPES.has(st.type.toUpperCase()))
  const ready = targets.length > 0 && places.length > 0
  const [databaseId, setDatabaseId] = useState(targets[0]?.id || '')
  const [storageId, setStorageId] = useState(places[0]?.id || '')
  const [coldError, setColdError] = useState('')
  const selectedDatabase = database?.id || databaseId || targets[0]?.id || ''
  const selectedStorage = storageId || places[0]?.id || ''
  const knownTables = [
    ...new Set(
      archives
        .filter((job) => job.status === 'success' && job.databaseId === selectedDatabase && job.storageId === selectedStorage && job.table)
        .map((job) => job.table),
    ),
  ]

  return (
    <form
      onSubmit={async (event) => {
        event.preventDefault()
        const data = Object.fromEntries(new FormData(event.currentTarget).entries()) as Record<string, string>
        setColdError('')
        try {
          const result = await api<{ rows: Record<string, unknown>[] }>('/api/cold-query', {
            method: 'POST',
            body: JSON.stringify({
              databaseId: data.databaseId,
              storageId: data.storageId,
              table: data.table,
              where: data.where,
              limit: Math.min(100, Math.max(1, Number(data.limit) || 100)),
            }),
          })
          setColdRows(result.rows || [])
        } catch (err) {
          setColdRows(null)
          setColdError(err instanceof Error ? err.message : String(err))
        }
      }}
    >
      <h1>{t.cold.title}</h1>
      <p className="muted">{t.cold.mysqlOnly}</p>
      <p className="muted">{t.cold.storageOnly}</p>
      <div className="split">
        <div>
          {database ? (
            <input type="hidden" name="databaseId" value={database.id} />
          ) : (
            <label>
              {t.common.database}
              <select
                name="databaseId"
                disabled={!targets.length}
                value={selectedDatabase}
                onChange={(event) => setDatabaseId(event.target.value)}
              >
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
            <select
              name="storageId"
              disabled={!places.length}
              value={selectedStorage}
              onChange={(event) => setStorageId(event.target.value)}
            >
              {places.map((st) => (
                <option key={st.id} value={st.id}>
                  {st.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t.archive.table}
            <input name="table" required list="cold-tables" />
          </label>
          <datalist id="cold-tables">
            {knownTables.map((table) => (
              <option key={table} value={table} />
            ))}
          </datalist>
          {knownTables.length ? <p className="muted">{t.cold.knownTables}</p> : null}
          <label>
            {t.cold.where}
            <input name="where" placeholder="id = 7" />
          </label>
          <label>
            {t.cold.limit}
            <input name="limit" type="number" min={1} max={100} defaultValue={100} />
          </label>
          <button className="primary" type="submit" disabled={!ready}>
            {t.cold.query}
          </button>
          {coldError ? <p className="err">{coldError}</p> : null}
        </div>
        <div className="card">
          {coldRows == null ? <p>{t.cold.hint}</p> : null}
          {coldRows && !coldRows.length ? <p>{t.cold.noRows}</p> : null}
          {coldRows && coldRows.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    {Object.keys(coldRows[0]).map((col) => (
                      <th key={col}>{col}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {coldRows.map((row, i) => (
                    <tr key={i}>
                      {Object.keys(coldRows[0]).map((col) => (
                        <td key={col}>{String(row[col] ?? '')}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      </div>
    </form>
  )
}
