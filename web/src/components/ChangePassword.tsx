import { useState } from 'react'
import type { FormEvent } from 'react'
import { api } from '../api'
import { useI18n } from '../i18n'
import { act } from '../lib/format'

type Props = {
  onChanged: () => void
}

export function ChangePassword({ onChanged }: Props) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')

  if (!open) {
    return (
      <button className="ghost" type="button" onClick={() => setOpen(true)}>
        {t.app.changePassword}
      </button>
    )
  }

  return (
    <form
      className="password-form"
      onSubmit={(event: FormEvent) => {
        event.preventDefault()
        act(async () => {
          await api('/api/session/password', {
            method: 'PUT',
            body: JSON.stringify({ current, password: next }),
          })
          alert(t.app.passwordChanged)
          setCurrent('')
          setNext('')
          setOpen(false)
          onChanged()
        })
      }}
    >
      <label>
        {t.app.currentPassword}
        <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
      </label>
      <label>
        {t.app.newPassword}
        <input type="password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} />
      </label>
      <div className="actions">
        <button className="primary" type="submit">
          {t.app.changePassword}
        </button>
        <button className="ghost" type="button" onClick={() => setOpen(false)}>
          {t.common.cancel}
        </button>
      </div>
    </form>
  )
}
