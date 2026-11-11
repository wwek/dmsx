import { useEffect, useState } from 'react'
import { api } from './api'
import type { Session } from './api'
import type { Section } from './constants/nav'
import { useAppData } from './hooks/useAppData'
import { useI18n } from './i18n'
import { AppLayout } from './layouts/AppLayout'
import { AuthGate } from './layouts/AuthGate'
import { act } from './lib/format'
import { errorMessage } from './lib/load'
import { AuditPage, DatabasesPage, NotifiersPage, OverviewPage, StoragesPage } from './pages'

export default function App() {
  const { t } = useI18n()
  const [session, setSession] = useState<Session | null>(null)
  const [section, setSection] = useState<Section>('overview')
  const [collapsed, setCollapsed] = useState(false)
  const [gateError, setGateError] = useState('')
  const { appData, refresh, healthOf, error, load } = useAppData(session)

  useEffect(() => {
    api<Session>('/api/session')
      .then(async (next) => {
        setSession(next)
        // A failed first load is shown by the layout, so the console does not
        // look like a machine without data.
        if (next.authenticated) await load()
      })
      .catch((err) => setGateError(errorMessage(err)))
  }, [load])

  if (!session) {
    return (
      <p className="muted" style={{ padding: 24 }}>
        {gateError || t.app.loading}
      </p>
    )
  }

  if (!session.authenticated) {
    return (
      <AuthGate
        session={session}
        onAuthenticated={async () => {
          setSession((current) => ({ configured: true, authenticated: true, version: current?.version }))
          // The first load after the password has to show its failure too.
          await load()
        }}
      />
    )
  }

  return (
    <AppLayout
      section={section}
      onSection={setSection}
      collapsed={collapsed}
      onToggleCollapsed={() => setCollapsed((value) => !value)}
      error={error}
      onLogout={() =>
        act(async () => {
          await api('/api/session', { method: 'DELETE' })
          setSession((current) => ({ configured: true, authenticated: false, version: current?.version }))
        })
      }
      onPasswordChanged={() =>
        setSession((current) => ({ configured: true, authenticated: false, version: current?.version }))
      }
      version={session.version || 'dev'}
    >
      {section === 'overview' && (
        <OverviewPage
          databases={appData.databases}
          storages={appData.storages}
          configs={appData.configs}
          runs={appData.runs}
          healthOf={healthOf}
          onSection={setSection}
        />
      )}
      {section === 'databases' && (
        <DatabasesPage
          databases={appData.databases}
          storages={appData.storages}
          configs={appData.configs}
          runs={appData.runs}
          restores={appData.restores}
          walSegments={appData.walSegments}
          archivePlans={appData.archivePlans}
          archives={appData.archives}
          ddls={appData.ddls}
          healthOf={healthOf}
          refresh={refresh}
          onSection={setSection}
        />
      )}
      {section === 'storages' && <StoragesPage storages={appData.storages} refresh={refresh} />}
      {section === 'notifiers' && (
        <NotifiersPage
          databases={appData.databases}
          notifiers={appData.notifiers}
          refresh={refresh}
        />
      )}
      {section === 'audit' && <AuditPage audit={appData.audit} />}
    </AppLayout>
  )
}
