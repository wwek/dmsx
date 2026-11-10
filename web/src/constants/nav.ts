export const SECTIONS = ['overview', 'databases', 'storages', 'notifiers', 'audit'] as const

export type Section = (typeof SECTIONS)[number]

// Backup destinations. Cold query is a separate, smaller set: DuckDB can open a
// local directory or an S3 API (S3, Qiniu, OSS, GCS). Azure, Swift and SSH cannot.
export const BACKUP_DESTINATIONS = [
  { id: 'LOCAL', cold: true },
  { id: 'S3', cold: true },
  { id: 'GCS', cold: true },
  { id: 'AZURE_BLOB', cold: false },
  { id: 'OSS', cold: true },
  { id: 'SWIFT', cold: false },
  { id: 'SSH', cold: false },
  { id: 'QINIU', cold: true },
] as const

export type BackupDestinationId = (typeof BACKUP_DESTINATIONS)[number]['id']

export const COLD_QUERY_TYPES = new Set<string>(BACKUP_DESTINATIONS.filter((item) => item.cold).map((item) => item.id))

export const STORAGE_TYPES = BACKUP_DESTINATIONS.map((item) => item.id)

export const NOTIFIER_EVENTS = [
  'BACKUP_SUCCESS',
  'BACKUP_FAILED',
  'HEALTH_UNAVAILABLE',
  'HEALTH_AVAILABLE',
  'RESTORE_SUCCESS',
  'RESTORE_FAILED',
  'ARCHIVE_SUCCESS',
  'ARCHIVE_FAILED',
  'DDL_SUCCESS',
  'DDL_FAILED',
] as const

export type NotifierEvent = (typeof NOTIFIER_EVENTS)[number]

export const DEFAULT_NOTIFIER_EVENTS: NotifierEvent[] = [
  'BACKUP_FAILED',
  'HEALTH_UNAVAILABLE',
  'HEALTH_AVAILABLE',
  'RESTORE_FAILED',
  'ARCHIVE_FAILED',
  'DDL_FAILED',
]
