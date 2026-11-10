import type { AuditEntry } from '../api'
import { useI18n } from '../i18n'

type Props = {
  audit: AuditEntry[]
}

export function AuditPage({ audit }: Props) {
  const { t, when } = useI18n()

  if (!audit.length) return <p className="muted">{t.audit.empty}</p>

  return (
    <>
      <h1>{t.audit.title}</h1>
      <div className="card table-wrap">
        <table>
          <thead>
            <tr>
              <th>{t.audit.time}</th>
              <th>{t.audit.action}</th>
              <th>{t.audit.target}</th>
            </tr>
          </thead>
          <tbody>
            {audit.map((row) => (
              <tr key={row.id}>
                <td>{when(row.at)}</td>
                <td>{t.audit.actions[row.action] || row.action}</td>
                <td>{row.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}
