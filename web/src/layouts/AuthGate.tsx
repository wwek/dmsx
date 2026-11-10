import { useState } from 'react'
import type { FormEvent } from 'react'
import { api } from '../api'
import type { Session } from '../api'
import { Preferences } from '../components/Preferences'
import { useI18n } from '../i18n'

type Props = {
  session: Session
  onAuthenticated: () => Promise<void>
}

export function AuthGate({ session, onAuthenticated }: Props) {
  const { t } = useI18n()
  const [password, setPassword] = useState('')
  const [gateError, setGateError] = useState('')
  const setup = !session.configured

  async function enter(event: FormEvent) {
    event.preventDefault()
    setGateError('')
    try {
      if (setup) await api('/api/setup', { method: 'POST', body: JSON.stringify({ password }) })
      else await api('/api/session', { method: 'POST', body: JSON.stringify({ password }) })
      setPassword('')
      await onAuthenticated()
    } catch (err) {
      setGateError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="gate">
      <form onSubmit={enter}>
        <Preferences />
        <p className="brand">{t.app.brand}</p>
        <h1>{setup ? t.gate.setupTitle : t.gate.loginTitle}</h1>
        <p className="muted">{setup ? t.gate.setupHint : t.gate.loginHint}</p>
        <label>
          {t.gate.password}
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            autoFocus
          />
        </label>
        {gateError ? <p className="err">{gateError}</p> : null}
        <button className="primary" type="submit">
          {setup ? t.gate.setupSubmit : t.gate.loginSubmit}
        </button>
      </form>
    </div>
  )
}
