import { useState } from 'react'
import type { FormEvent } from 'react'
import { api } from '../api'
import type { Storage } from '../api'
import { BACKUP_DESTINATIONS, COLD_QUERY_TYPES } from '../constants/nav'
import { useI18n } from '../i18n'

type Props = {
  storages: Storage[]
  refresh: () => Promise<void>
}

export function StoragesPage({ storages, refresh }: Props) {
  const { t } = useI18n()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [storageType, setStorageType] = useState('LOCAL')
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null)
  const edit = storages.find((row) => row.id === selectedId) || null
  const settings = edit?.settings && typeof edit.settings === 'object' ? edit.settings : {}
  const typeLabels = t.storages.types as Record<string, string>
  const typeName = (type: string) => typeLabels[type] || type
  const choices = BACKUP_DESTINATIONS.map((item) => item.id) as string[]
  if (edit && !choices.includes(edit.type)) choices.push(edit.type)
  const s3Family = ['S3', 'QINIU', 'OSS', 'GCS'].includes(storageType)
  const sshFamily = storageType === 'SSH' || storageType === 'SFTP'

  function payload(form: HTMLFormElement) {
    const data = Object.fromEntries(new FormData(form).entries()) as Record<string, string>
    const type = data.type
    const settingsObj: Record<string, string | boolean | number> = {}
    for (const [key, value] of Object.entries(data)) {
      if (['id', 'name', 'type'].includes(key)) continue
      settingsObj[key] = value
    }
    if (['S3', 'QINIU', 'OSS', 'GCS'].includes(type)) {
      settingsObj.virtualHost = (form.elements.namedItem('virtualHost') as HTMLInputElement | null)?.checked === true
      settingsObj.skipTLS = (form.elements.namedItem('skipTLS') as HTMLInputElement | null)?.checked === true
    }
    if (type === 'NAS') {
      settingsObj.port = Number(data.port || 445)
    }
    return { id: data.id, name: data.name, type, settings: settingsObj }
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const body = payload(form)
    setNotice(null)
    try {
      if (body.id) {
        await api(`/api/storages/${body.id}`, { method: 'PUT', body: JSON.stringify(body) })
        setNotice({ ok: true, text: t.storages.saved })
      } else {
        const created = await api<Storage>('/api/storages', { method: 'POST', body: JSON.stringify(body) })
        setSelectedId(created.id)
        setCreating(false)
        setNotice({ ok: true, text: t.storages.saved })
      }
      await refresh()
    } catch (err) {
      setNotice({ ok: false, text: err instanceof Error ? err.message : String(err) })
    }
  }

  async function onTest(form: HTMLFormElement) {
    const body = payload(form)
    setNotice(null)
    try {
      await api('/api/storages/probe', { method: 'POST', body: JSON.stringify(body) })
      setNotice({ ok: true, text: t.storages.testOk })
    } catch (err) {
      setNotice({ ok: false, text: err instanceof Error ? err.message : String(err) })
    }
  }

  const showForm = creating || !!edit || storages.length === 0

  return (
    <div className="catalog">
      <div className="list">
        <button
          className="primary"
          type="button"
          onClick={() => {
            setCreating(true)
            setSelectedId(null)
            setStorageType('LOCAL')
            setNotice(null)
          }}
        >
          {t.storages.add}
        </button>
        {storages.map((row) => (
          <article key={row.id} className={row.id === selectedId && !creating ? 'selected' : ''}>
            <button
              className="card-open"
              type="button"
              onClick={() => {
                setSelectedId(row.id)
                setCreating(false)
                setStorageType(row.type)
                setNotice(null)
              }}
            >
              <span className="row">
                <strong>{row.name}</strong>
                <span className="tag">
                  {typeName(row.type)}
                  {' · '}
                  {COLD_QUERY_TYPES.has(row.type) ? t.storages.coldCapable : t.storages.backupOnly}
                </span>
              </span>
            </button>
            <button
              className="danger"
              type="button"
              onClick={async (event) => {
                event.stopPropagation()
                setNotice(null)
                try {
                  await api(`/api/storages/${row.id}`, { method: 'DELETE' })
                  if (selectedId === row.id) setSelectedId(null)
                  await refresh()
                } catch (err) {
                  setNotice({ ok: false, text: err instanceof Error ? err.message : String(err) })
                }
              }}
            >
              {t.common.delete}
            </button>
          </article>
        ))}
        {!storages.length ? <p className="muted">{t.storages.empty}</p> : null}
      </div>
      <div>
        {!showForm ? <p className="muted">{t.storages.pick}</p> : null}
        {showForm ? (
      <form key={edit?.id || 'new'} onSubmit={onSubmit}>
        <h2>{edit ? t.storages.editTitle : t.storages.createTitle}</h2>
        <p className="muted">{t.storages.destHint}</p>
        <input type="hidden" name="id" defaultValue={edit?.id || ''} />
        <label>
          {t.common.name}
          <input name="name" required defaultValue={edit?.name || ''} />
        </label>
        <label>
          {t.common.type}
          <select
            name="type"
            value={storageType}
            disabled={!!edit}
            onChange={(e) => setStorageType(e.target.value)}
          >
            {choices.map((type) => (
              <option key={type} value={type}>
                {typeName(type)}
              </option>
            ))}
          </select>
        </label>
        {edit ? <input type="hidden" name="type" value={storageType} /> : null}
        {storageType === 'LOCAL' && (
          <label>
            {t.storages.path}
            <input name="path" defaultValue={String(settings.path || '')} required={!edit} />
          </label>
        )}
        {s3Family && (
          <>
            <label>
              Bucket
              <input name="bucket" defaultValue={String(settings.bucket || '')} />
            </label>
            <label>
              Region
              <input name="region" defaultValue={String(settings.region || '')} />
            </label>
            <label>
              Endpoint
              <input name="endpoint" defaultValue={String(settings.endpoint || '')} />
            </label>
            <label>
              Access Key
              <input name="accessKey" defaultValue={String(settings.accessKey || '')} />
            </label>
            <label>
              Secret Key
              <input name="secretKey" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.storages.prefix}
              <input name="prefix" defaultValue={String(settings.prefix || '')} />
            </label>
            <label className="check">
              <input name="virtualHost" type="checkbox" defaultChecked={!!settings.virtualHost} />
              <span>{t.storages.virtualHost}</span>
            </label>
            <label className="check">
              <input name="skipTLS" type="checkbox" defaultChecked={!!settings.skipTLS} />
              <span>{t.storages.skipTLS}</span>
            </label>
            <label>
              {t.storages.storageClass}
              <input name="storageClass" defaultValue={String(settings.storageClass || '')} />
            </label>
          </>
        )}
        {(sshFamily || storageType === 'FTP') && (
          <>
            <label>
              {t.common.host}
              <input name="host" defaultValue={String(settings.host || '')} />
            </label>
            <label>
              {t.common.port}
              <input name="port" type="number" defaultValue={Number(settings.port || (storageType === 'FTP' ? 21 : 22))} />
            </label>
            <label>
              {t.common.user}
              <input name="user" defaultValue={String(settings.user || '')} />
            </label>
            <label>
              {t.common.password}
              <input name="password" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            {sshFamily ? (
              <label>
                {t.storages.privateKey}
                <textarea name="key" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
              </label>
            ) : null}
            <label>
              {t.storages.path}
              <input name="path" defaultValue={String(settings.path || '')} />
            </label>
            <label>
              {t.storages.prefix}
              <input name="prefix" defaultValue={String(settings.prefix || '')} />
            </label>
          </>
        )}
        {storageType === 'SWIFT' && (
          <>
            <label>
              {t.storages.authUrl}
              <input name="authUrl" defaultValue={String(settings.authUrl || '')} placeholder="https://cloud.example:5000" />
            </label>
            <label>
              {t.common.user}
              <input name="user" defaultValue={String(settings.user || '')} />
            </label>
            <label>
              {t.common.password}
              <input name="password" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.storages.project}
              <input name="project" defaultValue={String(settings.project || '')} />
            </label>
            <label>
              {t.storages.container}
              <input name="container" defaultValue={String(settings.container || '')} />
            </label>
            <label>
              {t.storages.prefix}
              <input name="prefix" defaultValue={String(settings.prefix || '')} />
            </label>
          </>
        )}
        {storageType === 'AZURE_BLOB' && (
          <>
            <label>
              Account
              <input name="account" defaultValue={String(settings.account || '')} />
            </label>
            <label>
              Account Key
              <input name="accountKey" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              Container
              <input name="container" defaultValue={String(settings.container || '')} />
            </label>
            <label>
              Endpoint
              <input name="endpoint" defaultValue={String(settings.endpoint || '')} />
            </label>
            <label>
              {t.storages.prefix}
              <input name="prefix" defaultValue={String(settings.prefix || '')} />
            </label>
          </>
        )}
        {storageType === 'GOOGLE_DRIVE' && (
          <>
            <label>
              Access Token
              <input name="accessToken" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              Folder ID
              <input name="folderId" defaultValue={String(settings.folderId || '')} />
            </label>
            <label>
              {t.storages.prefix}
              <input name="prefix" defaultValue={String(settings.prefix || '')} />
            </label>
            <p className="muted">{t.storages.driveStub}</p>
          </>
        )}
        {storageType === 'NAS' && (
          <>
            <label>
              {t.common.host}
              <input name="host" defaultValue={String(settings.host || '')} />
            </label>
            <label>
              {t.common.port}
              <input name="port" type="number" defaultValue={Number(settings.port || 445)} />
            </label>
            <label>
              Share
              <input name="share" defaultValue={String(settings.share || '')} />
            </label>
            <label>
              {t.storages.domain}
              <input name="domain" defaultValue={String(settings.domain || '')} />
            </label>
            <label>
              {t.common.user}
              <input name="user" defaultValue={String(settings.user || '')} />
            </label>
            <label>
              {t.common.password}
              <input name="password" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.storages.path}
              <input name="path" defaultValue={String(settings.path || '')} />
            </label>
            <label>
              {t.storages.prefix}
              <input name="prefix" defaultValue={String(settings.prefix || '')} />
            </label>
          </>
        )}
        {storageType === 'RCLONE' && (
          <>
            <label>
              Remote
              <input name="remote" defaultValue={String(settings.remote || '')} placeholder="qiniu:bucket" />
            </label>
            <label>
              Bin
              <input name="bin" defaultValue={String(settings.bin || 'rclone')} />
            </label>
          </>
        )}
        <p className="muted">{t.storages.secretsHint}</p>
        {notice ? <p className={notice.ok ? 'notice ok' : 'notice bad'}>{notice.text}</p> : null}
        <div className="actions">
          <button
            className="primary"
            type="button"
            onClick={(event) => {
              const form = event.currentTarget.form
              if (form) void onTest(form)
            }}
          >
            {t.databases.testConnection}
          </button>
          <button className="ghost" type="submit">
            {edit ? t.common.saveChanges : t.common.save}
          </button>
          {creating ? (
            <button className="ghost" type="button" onClick={() => setCreating(false)}>
              {t.common.cancelEdit}
            </button>
          ) : null}
        </div>
      </form>
        ) : null}
      </div>
    </div>
  )
}
