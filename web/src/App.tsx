import { useEffect, useState } from 'react'
import { api } from './api'
import type { Session } from './api'
import type { Section } from './constants/nav'
import { useAppData } from './hooks/useAppData'
import { useI18n } from './i18n'
import { AppLayout } from './layouts/AppLayout'
import { AuthGate } from './layouts/AuthGate'
import { act } from './lib/format'
import { AuditPage, DatabasesPage, NotifiersPage, OverviewPage, StoragesPage } from './pages'

export default function App() {
  const { t } = useI18n()
  const [session, setSession] = useState<Session | null>(null)
  const [section, setSection] = useState<Section>('overview')
  const [collapsed, setCollapsed] = useState(false)
  const [gateError, setGateError] = useState('')
  const { appData, refresh, healthOf } = useAppData(session, section)

  useEffect(() => {
    api<Session>('/api/session')
      .then(async (next) => {
        setSession(next)
        if (next.authenticated) await refresh()
      })
      .catch((err) => setGateError(err.message))
  }, [refresh])

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
          await refresh()
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
