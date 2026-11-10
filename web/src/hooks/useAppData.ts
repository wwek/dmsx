import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import type {
  ArchiveJob,
  ArchivePlan,
  AuditEntry,
  BackupConfig,
  BackupRun,
  Database,
  DdlRun,
  HealthCheck,
  Notifier,
  RestoreRun,
  Session,
  Storage,
  WalSegment,
} from '../api'
import type { Section } from '../constants/nav'

export type AppData = {
  databases: Database[]
  storages: Storage[]
  configs: BackupConfig[]
  runs: BackupRun[]
  restores: RestoreRun[]
  archivePlans: ArchivePlan[]
  archives: ArchiveJob[]
  ddls: DdlRun[]
  notifiers: Notifier[]
  health: HealthCheck[]
  audit: AuditEntry[]
  walSegments: WalSegment[]
}

const empty: AppData = {
  databases: [],
  storages: [],
  configs: [],
  runs: [],
  restores: [],
  archivePlans: [],
  archives: [],
  ddls: [],
  notifiers: [],
  health: [],
  audit: [],
  walSegments: [],
}

export function useAppData(session: Session | null, section: Section) {
  const [appData, setAppData] = useState<AppData>(empty)

  const refresh = useCallback(async () => {
    const [
      databases,
      storages,
      configs,
      runs,
      restores,
      archivePlans,
      archives,
      ddls,
      notifiers,
      health,
      audit,
      walSegments,
    ] = await Promise.all([
      api<Database[]>('/api/databases'),
      api<Storage[]>('/api/storages'),
      api<BackupConfig[]>('/api/backups/configs'),
      api<BackupRun[]>('/api/backups/runs'),
      api<RestoreRun[]>('/api/backups/restores'),
      api<ArchivePlan[]>('/api/archive/plans'),
      api<ArchiveJob[]>('/api/archive/jobs'),
      api<DdlRun[]>('/api/ddl/runs'),
      api<Notifier[]>('/api/notifiers'),
      api<HealthCheck[]>('/api/healthchecks'),
      api<AuditEntry[]>('/api/audit'),
      api<WalSegment[]>('/api/wal/segments'),
    ])
    setAppData({
      databases,
      storages,
      configs,
      runs,
      restores,
      archivePlans,
      archives,
      ddls,
      notifiers,
      health,
      audit,
      walSegments,
    })
  }, [])

  useEffect(() => {
    if (!session?.authenticated) return
    const timer = setInterval(() => {
      if (section === 'overview' || section === 'databases' || section === 'notifiers') {
        refresh().catch(() => {})
      }
    }, 5000)
    return () => clearInterval(timer)
  }, [session, section, refresh])

  const healthOf = useCallback(
    (id: string) =>
      appData.health.find((row) => row.databaseId === id) || {
        databaseId: id,
        status: '',
        attempts: [],
        checkedAt: '',
      },
    [appData.health],
  )

  return { appData, refresh, healthOf, setAppData }
}
