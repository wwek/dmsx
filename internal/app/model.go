package app

import (
	"encoding/json"
	"time"

	"dmsx/internal/healthcheck"
	"dmsx/internal/retention"
	"dmsx/internal/schedule"
)

const (
	kindDatabase = "database"
	kindStorage  = "storage"
	kindConfig   = "backup_config"
	kindRun      = "backup_run"
	kindArchive  = "archive_job"
	kindDDL      = "ddl_run"
	kindOperator = "operator"
	kindRestore  = "restore"
	kindNotifier = "notifier"
	kindHealth   = "health_state"
	kindAudit    = "audit"
)

type Database struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Engine          string `json:"engine"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"password,omitempty"`
	HasPassword     bool   `json:"hasPassword"`
	Database        string `json:"database"`
	SSL             bool   `json:"ssl"`
	Version         string `json:"version"`
	IncludeTriggers bool   `json:"includeTriggers"`
	IncludeEvents   bool   `json:"includeEvents"`
	SSH             *SSH   `json:"ssh,omitempty"`
	// HealthCheck is nil for the default: probe every minute, three failures
	// before unavailable, notify on change.
	HealthCheck *healthcheck.Config `json:"healthCheck,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
}

func (d Database) healthConfig() healthcheck.Config {
	if d.HealthCheck == nil {
		return healthcheck.Default()
	}
	return *d.HealthCheck
}

// Notifier is one delivery channel. Empty DatabaseIDs means every database.
type Notifier struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Settings    json.RawMessage `json:"settings"`
	Events      []string        `json:"events"`
	DatabaseIDs []string        `json:"databaseIds"`
	LastSentAt  *time.Time      `json:"lastSentAt,omitempty"`
	LastError   string          `json:"lastError"`
	CreatedAt   time.Time       `json:"createdAt"`
}

type AuditEntry struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

type SSH struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
	Key      string `json:"key,omitempty"`
}

type Storage struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt time.Time       `json:"createdAt"`
}

type BackupConfig struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	DatabaseID     string            `json:"databaseId"`
	StorageID      string            `json:"storageId"`
	Method         string            `json:"method"`
	Schedule       schedule.Schedule `json:"schedule"`
	Retention      retention.Policy  `json:"retention"`
	Encrypted      bool              `json:"encrypted"`
	ExcludeTables  []string          `json:"excludeTables"`
	IncludeSchemas []string          `json:"includeSchemas"`
	ExcludeData    []string          `json:"excludeTableData"`
	PhysicalCodec  string            `json:"physicalCodec"`
	// MaxBackupSizeMB stops one backup that grows past it. MaxTotalSizeMB
	// deletes this plan's oldest backups once their sum passes it. 0 is no cap.
	MaxBackupSizeMB int64      `json:"maxBackupSizeMb"`
	MaxTotalSizeMB  int64      `json:"maxTotalSizeMb"`
	Enabled         bool       `json:"enabled"`
	Retry           bool       `json:"retry"`
	MaxFailedTries  int        `json:"maxFailedTries"`
	FailCount       int        `json:"failCount"`
	LastRunAt       *time.Time `json:"lastRunAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type BackupRun struct {
	ID         string     `json:"id"`
	ConfigID   string     `json:"configId"`
	DatabaseID string     `json:"databaseId"`
	StorageID  string     `json:"storageId"`
	Status     string     `json:"status"`
	FileName   string     `json:"fileName"`
	Codec      string     `json:"codec"`
	Encrypted  bool       `json:"encrypted"`
	Bytes      int64      `json:"bytes"`
	SHA256     string     `json:"sha256"`
	Error      string     `json:"error"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type RestoreTarget struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSL      bool   `json:"ssl"`
}

type RestoreRun struct {
	ID         string     `json:"id"`
	BackupID   string     `json:"backupId"`
	DatabaseID string     `json:"databaseId"`
	Engine     string     `json:"engine"`
	Database   string     `json:"database"`
	Status     string     `json:"status"`
	Error      string     `json:"error"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type ArchiveJob struct {
	ID           string     `json:"id"`
	DatabaseID   string     `json:"databaseId"`
	StorageID    string     `json:"storageId"`
	Table        string     `json:"table"`
	Where        string     `json:"where"`
	Year         int        `json:"year"`
	BatchSize    int        `json:"batchSize"`
	PauseMS      int        `json:"pauseMs"`
	DeleteAfter  bool       `json:"deleteAfter"`
	Status       string     `json:"status"`
	ObjectKey    string     `json:"objectKey"`
	MD5          string     `json:"md5"`
	RowsArchived int64      `json:"rowsArchived"`
	RowsDeleted  int64      `json:"rowsDeleted"`
	Error        string     `json:"error"`
	Log          string     `json:"log"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
}

type DDLRun struct {
	ID         string     `json:"id"`
	DatabaseID string     `json:"databaseId"`
	Statement  string     `json:"statement"`
	Table      string     `json:"table"`
	Alter      string     `json:"alter"`
	Tool       string     `json:"tool"`
	Execute    bool       `json:"execute"`
	ChunkSize  int        `json:"chunkSize"`
	Status     string     `json:"status"`
	Copied     int64      `json:"copied"`
	Total      int64      `json:"total"`
	Percent    float64    `json:"percent"`
	ETA        string     `json:"eta"`
	Error      string     `json:"error"`
	Log        string     `json:"log"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type ColdRequest struct {
	DatabaseID string `json:"databaseId"`
	StorageID  string `json:"storageId"`
	Table      string `json:"table"`
	Where      string `json:"where"`
	Limit      int    `json:"limit"`
}
