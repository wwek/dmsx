export type Session = {
  configured: boolean
  authenticated: boolean
  version?: string
}

export type HealthCheckConfig = {
  enabled: boolean
  notify: boolean
  intervalMinutes: number
  failuresBeforeDown: number
}

export type Database = {
  id: string
  name: string
  engine: string
  host: string
  port: number
  username: string
  database: string
  version?: string
  ssl?: boolean
  includeTriggers?: boolean
  includeEvents?: boolean
  healthCheck?: HealthCheckConfig
  ssh?: {
    host?: string
    port?: number
    user?: string
  }
}

export type Storage = {
  id: string
  name: string
  type: string
  settings?: Record<string, string | number | boolean>
}

export type HealthAttempt = {
  status: string
  at?: string
  error?: string
}

export type HealthCheck = {
  databaseId: string
  status: string
  checkedAt?: string
  attempts?: HealthAttempt[]
}

export type BackupConfig = {
  id: string
  name: string
  databaseId: string
  storageId: string
  method: string
  enabled?: boolean
  encrypted?: boolean
  retry?: boolean
  schedule?: {
    type?: string
    timeOfDay?: string
    cronExpression?: string
    weekday?: number
    dayOfMonth?: number
  }
  retention?: {
    kind?: string
    period?: string
    count?: number
    gfs?: {
      hours: number
      days: number
      weeks: number
      months: number
      years: number
    }
  }
  excludeTables?: string[]
  includeSchemas?: string[]
  excludeTableData?: string[]
  maxBackupSizeMb?: number
  maxTotalSizeMb?: number
  maxFailedTries?: number
  failCount?: number
  lastRunAt?: string
  physicalMode?: string
  physicalCodec?: string
  walEnabled?: boolean
}

export type BackupRun = {
  id: string
  configId?: string
  databaseId?: string
  storageId?: string
  status: string
  startedAt?: string
  finishedAt?: string
  fileName?: string
  bytes?: number
  codec?: string
  encrypted?: boolean
  sha256?: string
  error?: string
  physicalKind?: string
  startLsn?: string
  stopLsn?: string
  timelineId?: number
  parentRunId?: string
  rootRunId?: string
  forceFull?: boolean
  systemId?: number
  manifestKey?: string
}

export type RestoreRun = {
  id: string
  backupId?: string
  databaseId?: string
  engine?: string
  database: string
  status: string
  error?: string
  report?: string
  startedAt?: string
  finishedAt?: string
}

export type ArchivePlan = {
  id: string
  name: string
  databaseId: string
  storageId: string
  lastRunAt?: string
  table: string
  where: string
  year: number
  batchSize: number
  pauseMs?: number
  deleteAfter?: boolean
  enabled?: boolean
  schedule?: {
    type?: string
    timeOfDay?: string
    cronExpression?: string
    weekday?: number
    dayOfMonth?: number
  }
}

export type ArchiveJob = {
  id: string
  planId?: string
  databaseId?: string
  storageId?: string
  table: string
  year: number
  status: string
  phase?: string
  objectKey?: string
  objectKeys?: string[]
  md5?: string
  rowsArchived?: number
  rowsDeleted?: number
  error?: string
  startedAt?: string
  finishedAt?: string
}

export type WalSegment = {
  id: string
  databaseId: string
  name: string
  objectKey: string
  bytes: number
  timelineId?: number
  uploadedAt?: string
}

export type DdlRun = {
  id: string
  databaseId?: string
  table?: string
  statement?: string
  tool?: string
  execute?: boolean
  chunkSize?: number
  status: string
  copied?: number
  total?: number
  percent?: number
  eta?: string
  error?: string
  log?: string
  startedAt?: string
  finishedAt?: string
}

export type Notifier = {
  id: string
  name: string
  type: string
  settings?: Record<string, unknown>
  events?: string[]
  databaseIds?: string[]
  lastSentAt?: string
  lastError?: string
}

export type AuditEntry = {
  id: string
  at: string
  action: string
  detail?: string
}
