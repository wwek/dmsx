import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { api } from '../api'
import type { Database, Notifier } from '../api'
import { DEFAULT_NOTIFIER_EVENTS, NOTIFIER_EVENTS } from '../constants/nav'
import type { NotifierEvent } from '../constants/nav'
import { useI18n } from '../i18n'

type Props = {
  databases: Database[]
  notifiers: Notifier[]
  refresh: () => Promise<void>
}

function settingsOf(edit: Notifier | null): Record<string, unknown> {
  const raw = edit?.settings
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) return raw as Record<string, unknown>
  return {}
}

function strSetting(settings: Record<string, unknown>, key: string): string {
  const value = settings[key]
  return value == null ? '' : String(value)
}

function settingsFromForm(type: string, fd: FormData): Record<string, unknown> {
  const str = (key: string) => String(fd.get(key) || '')
  switch (type) {
    case 'EMAIL':
      return {
        host: str('host'),
        port: Number(str('port') || 587),
        user: str('user'),
        password: str('password'),
        from: str('from'),
        to: str('to'),
        security: str('security') || 'STARTTLS',
        insecureSkipVerify: fd.get('insecureSkipVerify') === 'on',
      }
    case 'TELEGRAM':
      return { botToken: str('botToken'), chatId: str('chatId'), apiUrl: str('apiUrl') }
    case 'SLACK':
      return { webhookUrl: str('webhookUrl'), botToken: str('botToken'), channel: str('channel') }
    case 'DISCORD':
    case 'TEAMS':
      return { webhookUrl: str('webhookUrl') }
    case 'MATTERMOST':
      return {
        webhookUrl: str('webhookUrl'),
        channel: str('channel'),
        serverUrl: str('serverUrl'),
        botToken: str('botToken'),
        channelId: str('channelId'),
      }
    case 'WEBHOOK':
      return {
        url: str('url'),
        method: str('method') || 'POST',
        headers: str('headers'),
        bodyTemplate: str('bodyTemplate'),
      }
    default:
      return {}
  }
}

export function NotifiersPage({ databases, notifiers, refresh }: Props) {
  const { t, when } = useI18n()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [type, setType] = useState('TELEGRAM')
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null)
  const edit = notifiers.find((row) => row.id === selectedId) || null
  const settings = settingsOf(edit)
  const selectedEvents = edit?.events?.length ? edit.events : DEFAULT_NOTIFIER_EVENTS
  const selectedDatabases = edit?.databaseIds || []
  useEffect(() => {
    if (selectedId && notifiers.some((row) => row.id === selectedId)) setCreating(false)
  }, [selectedId, notifiers])

  function bodyFrom(form: HTMLFormElement) {
    const fd = new FormData(form)
    const selectedType = String(fd.get('type') || type)
    return {
      id: String(fd.get('id') || ''),
      name: String(fd.get('name') || ''),
      type: selectedType,
      settings: settingsFromForm(selectedType, fd),
      events: fd.getAll('event'),
      databaseIds: fd.getAll('databaseId'),
    }
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const body = bodyFrom(form)
    setNotice(null)
    try {
      if (body.id) {
        await api(`/api/notifiers/${body.id}`, { method: 'PUT', body: JSON.stringify(body) })
        setNotice({ ok: true, text: t.notifiers.saved })
      } else {
        const created = await api<Notifier>('/api/notifiers', { method: 'POST', body: JSON.stringify(body) })
        setSelectedId(created.id)
        setType(created.type)
      }
      await refresh()
    } catch (err) {
      setNotice({ ok: false, text: err instanceof Error ? err.message : String(err) })
    }
  }

  async function onTest(form: HTMLFormElement) {
    const body = bodyFrom(form)
    setNotice(null)
    try {
      await api('/api/notifiers/probe', { method: 'POST', body: JSON.stringify(body) })
      setNotice({ ok: true, text: t.notifiers.testOk })
      if (body.id) await refresh()
    } catch (err) {
      setNotice({ ok: false, text: err instanceof Error ? err.message : String(err) })
      if (body.id) await refresh()
    }
  }

  const showForm = creating || !!edit || notifiers.length === 0

  return (
    <div className="catalog">
      <div className="list">
        <button
          className="primary"
          type="button"
          onClick={() => {
            setCreating(true)
            setSelectedId(null)
            setType('TELEGRAM')
            setNotice(null)
          }}
        >
          {t.notifiers.add}
        </button>
        {notifiers.map((row) => (
          <article key={row.id} className={row.id === selectedId && !creating ? 'selected' : ''}>
            <button
              className="card-open"
              type="button"
              onClick={() => {
                setSelectedId(row.id)
                setCreating(false)
                setType(row.type)
                setNotice(null)
              }}
            >
              <span className="row">
                <strong>{row.name}</strong>
                <span className="tag">{row.type}</span>
              </span>
              {row.lastSentAt ? <span className="muted">{when(row.lastSentAt)}</span> : null}
              {row.lastError ? <span className="err">{row.lastError}</span> : null}
            </button>
            <button
              className="danger"
              type="button"
              onClick={async () => {
                setNotice(null)
                try {
                  await api(`/api/notifiers/${row.id}`, { method: 'DELETE' })
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
        {!notifiers.length ? <p className="muted">{t.notifiers.empty}</p> : null}
      </div>
      <div>
        {!showForm ? <p className="muted">{t.notifiers.pick}</p> : null}
        {showForm ? (
      <form key={edit?.id || 'new'} onSubmit={onSubmit}>
        <h2>{edit ? t.notifiers.editTitle : t.notifiers.title}</h2>
        <input type="hidden" name="id" defaultValue={edit?.id || ''} />
        <label>
          {t.common.name}
          <input name="name" required defaultValue={edit?.name || ''} />
        </label>
        <label>
          {t.common.type}
          <select
            name="type"
            value={edit ? edit.type : type}
            disabled={!!edit}
            onChange={(e) => {
              if (!edit) setType(e.target.value)
            }}
          >
            {['EMAIL', 'TELEGRAM', 'SLACK', 'DISCORD', 'TEAMS', 'MATTERMOST', 'WEBHOOK'].map((item) => (
              <option key={item}>{item}</option>
            ))}
          </select>
        </label>
        {edit ? <input type="hidden" name="type" value={edit.type} /> : null}
        {(edit ? edit.type : type) === 'EMAIL' && (
          <>
            <label>
              {t.notifiers.emailHost}
              <input name="host" required defaultValue={strSetting(settings, 'host')} />
            </label>
            <label>
              {t.notifiers.emailPort}
              <input name="port" type="number" defaultValue={Number(settings.port || 587)} />
            </label>
            <label>
              {t.notifiers.emailUser}
              <input name="user" defaultValue={strSetting(settings, 'user')} />
            </label>
            <label>
              {t.notifiers.emailPassword}
              <input name="password" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.notifiers.emailFrom}
              <input name="from" defaultValue={strSetting(settings, 'from')} />
            </label>
            <label>
              {t.notifiers.emailTo}
              <input name="to" required defaultValue={strSetting(settings, 'to')} />
            </label>
            <label>
              {t.notifiers.emailSecurity}
              <select name="security" defaultValue={strSetting(settings, 'security') || 'STARTTLS'}>
                <option>STARTTLS</option>
                <option>TLS</option>
                <option>NONE</option>
              </select>
            </label>
            <label className="check">
              <input
                name="insecureSkipVerify"
                type="checkbox"
                defaultChecked={settings.insecureSkipVerify === true}
              />
              <span>insecureSkipVerify</span>
            </label>
          </>
        )}
        {(edit ? edit.type : type) === 'TELEGRAM' && (
          <>
            <label>
              {t.notifiers.botToken}
              <input name="botToken" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.notifiers.chatId}
              <input name="chatId" defaultValue={strSetting(settings, 'chatId')} />
            </label>
            <label>
              {t.notifiers.apiUrl}
              <input name="apiUrl" defaultValue={strSetting(settings, 'apiUrl')} placeholder="https://api.telegram.org" />
            </label>
          </>
        )}
        {(edit ? edit.type : type) === 'SLACK' && (
          <>
            <label>
              {t.notifiers.webhookUrl}
              <input name="webhookUrl" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.notifiers.botToken}
              <input name="botToken" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.notifiers.channel}
              <input name="channel" defaultValue={strSetting(settings, 'channel')} />
            </label>
          </>
        )}
        {((edit ? edit.type : type) === 'DISCORD' || (edit ? edit.type : type) === 'TEAMS') && (
          <label>
            {t.notifiers.webhookUrl}
            <input
              name="webhookUrl"
              type="password"
              required={!edit}
              placeholder={edit ? t.common.emptyPasswordKeep : ''}
            />
          </label>
        )}
        {(edit ? edit.type : type) === 'MATTERMOST' && (
          <>
            <label>
              {t.notifiers.webhookUrl}
              <input name="webhookUrl" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.notifiers.channel}
              <input name="channel" defaultValue={strSetting(settings, 'channel')} />
            </label>
            <label>
              {t.notifiers.serverUrl}
              <input name="serverUrl" defaultValue={strSetting(settings, 'serverUrl')} />
            </label>
            <label>
              {t.notifiers.botToken}
              <input name="botToken" type="password" placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.notifiers.channelId}
              <input name="channelId" defaultValue={strSetting(settings, 'channelId')} />
            </label>
          </>
        )}
        {(edit ? edit.type : type) === 'WEBHOOK' && (
          <>
            <label>
              {t.notifiers.url}
              <input name="url" required defaultValue={strSetting(settings, 'url')} />
            </label>
            <label>
              {t.notifiers.method}
              <select name="method" defaultValue={strSetting(settings, 'method') || 'POST'}>
                <option>POST</option>
                <option>GET</option>
              </select>
            </label>
            <label>
              {t.notifiers.headers}
              <textarea name="headers" defaultValue={strSetting(settings, 'headers')} placeholder={edit ? t.common.emptyPasswordKeep : ''} />
            </label>
            <label>
              {t.notifiers.bodyTemplate}
              <textarea
                name="bodyTemplate"
                defaultValue={strSetting(settings, 'bodyTemplate')}
                placeholder='{"text":"{{heading}}\n{{message}}"}'
              />
            </label>
          </>
        )}
        <fieldset>
          <legend>{t.notifiers.eventsLegend}</legend>
          {NOTIFIER_EVENTS.map((id) => (
            <label className="check" key={id}>
              <input
                type="checkbox"
                name="event"
                value={id}
                defaultChecked={selectedEvents.includes(id as NotifierEvent) || selectedEvents.includes(id)}
              />
              <span>{t.notifiers.events[id]}</span>
            </label>
          ))}
        </fieldset>
        <fieldset>
          <legend>{t.notifiers.databasesLegend}</legend>
          {databases.map((db) => (
            <label className="check" key={db.id}>
              <input
                type="checkbox"
                name="databaseId"
                value={db.id}
                defaultChecked={selectedDatabases.includes(db.id)}
              />
              <span>{db.name}</span>
            </label>
          ))}
        </fieldset>
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
            {t.notifiers.testMessage}
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
