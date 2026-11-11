import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { loadWithError } from '../lib/load'
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

export function useAppData(session: Session | null) {
  const [appData, setAppData] = useState<AppData>(empty)
  const [error, setError] = useState('')

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

  // load keeps the message of a failed load, so the console can show it
  // instead of an empty page. The pages call refresh, which throws, so that a
  // failed action of the operator still reports itself.
  const load = useCallback(async () => {
    await loadWithError(refresh, setError)
  }, [refresh])

  useEffect(() => {
    if (!session?.authenticated) return
    // The data is the same in every section, so every section refreshes it.
    // A page that was left empty by a failed load gets it back this way.
    const timer = setInterval(() => {
      void load()
    }, 5000)
    return () => clearInterval(timer)
  }, [session, load])

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

  return { appData, refresh, healthOf, setAppData, error, load }
}
