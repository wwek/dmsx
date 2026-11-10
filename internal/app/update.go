package app

import (
	"context"
	"encoding/json"
	"fmt"

	"dmsx/internal/engine"
	"dmsx/internal/sqlident"
	"dmsx/internal/storage"
)

// UpdateDatabase changes a registered connection. An empty password or empty
// SSH secret keeps the sealed value already in the metadata repo.
func (a *App) UpdateDatabase(ctx context.Context, id string, in Database) (Database, error) {
	existing, err := a.database(ctx, id)
	if err != nil {
		return Database{}, err
	}
	if err := checkEngine(in.Engine); err != nil {
		return Database{}, err
	}
	if in.Name == "" || in.Host == "" || in.Username == "" || in.Database == "" || in.Port <= 0 {
		return Database{}, fmt.Errorf("name, host, port, username and database are required")
	}
	if !sqlident.Valid(in.Database) {
		return Database{}, fmt.Errorf("database name must be a plain identifier")
	}
	if in.HealthCheck == nil {
		in.HealthCheck = existing.HealthCheck
	} else if err := in.HealthCheck.Validate(); err != nil {
		return Database{}, err
	}
	in.ID = existing.ID
	in.CreatedAt = existing.CreatedAt
	if in.Password == "" {
		in.Password = existing.Password
	}
	if in.SSH == nil {
		in.SSH = existing.SSH
	} else if existing.SSH != nil {
		if in.SSH.Password == "" {
			in.SSH.Password = existing.SSH.Password
		}
		if in.SSH.Key == "" {
			in.SSH.Key = existing.SSH.Key
		}
	}
	in.HasPassword = in.Password != ""
	if err := a.sealDatabase(&in); err != nil {
		return Database{}, err
	}
	if err := a.repo.Put(ctx, kindDatabase, in.ID, in); err != nil {
		return Database{}, err
	}
	a.Audit(ctx, "database.update", fmt.Sprintf("%s (%s %s:%d/%s)", in.Name, in.Engine, in.Host, in.Port, in.Database))
	return in.Public(), nil
}

// UpdateStorage changes a destination. Blank secret settings keep the previous
// sealed values. The type cannot change.
func (a *App) UpdateStorage(ctx context.Context, id string, in Storage) (Storage, error) {
	var existing Storage
	if err := a.repo.Get(ctx, kindStorage, id, &existing); err != nil {
		return Storage{}, err
	}
	if in.Name == "" {
		return Storage{}, fmt.Errorf("name is required")
	}
	if in.Type == "" {
		in.Type = existing.Type
	}
	if in.Type != existing.Type {
		return Storage{}, fmt.Errorf("storage type cannot change")
	}
	opened, err := a.openSettings(existing.Settings, storageSecrets)
	if err != nil {
		return Storage{}, err
	}
	merged, err := mergeSettings(opened, in.Settings, storageSecrets)
	if err != nil {
		return Storage{}, err
	}
	if _, err := storage.New(in.Type, merged); err != nil {
		return Storage{}, err
	}
	sealed, err := a.sealSettings(merged, storageSecrets)
	if err != nil {
		return Storage{}, err
	}
	in.ID = existing.ID
	in.CreatedAt = existing.CreatedAt
	in.Settings = sealed
	if err := a.repo.Put(ctx, kindStorage, in.ID, in); err != nil {
		return Storage{}, err
	}
	a.Audit(ctx, "storage.update", in.Name)
	masked, err := a.openSettings(sealed, storageSecrets)
	if err != nil {
		return Storage{}, err
	}
	in.Settings = maskSettings(masked, storageSecrets)
	return in, nil
}

// UpdateConfig replaces a backup plan in place. FailCount and LastRunAt stay.
func (a *App) UpdateConfig(ctx context.Context, id string, in BackupConfig) (BackupConfig, error) {
	var existing BackupConfig
	if err := a.repo.Get(ctx, kindConfig, id, &existing); err != nil {
		return BackupConfig{}, err
	}
	if err := a.validateConfig(ctx, &in); err != nil {
		return BackupConfig{}, err
	}
	in.ID = existing.ID
	in.CreatedAt = existing.CreatedAt
	in.FailCount = existing.FailCount
	in.LastRunAt = existing.LastRunAt
	if err := a.repo.Put(ctx, kindConfig, in.ID, in); err != nil {
		return BackupConfig{}, err
	}
	a.Audit(ctx, "backup_plan.update", in.Name+" for "+a.databaseName(ctx, in.DatabaseID))
	return in, nil
}

func (a *App) validateConfig(ctx context.Context, in *BackupConfig) error {
	if _, err := a.database(ctx, in.DatabaseID); err != nil {
		return err
	}
	if _, _, err := a.store(ctx, in.StorageID); err != nil {
		return err
	}
	if in.Method == "" {
		in.Method = "logical"
	}
	if in.Method != "logical" && in.Method != "physical" {
		return fmt.Errorf("method must be logical or physical")
	}
	if in.Method == "physical" {
		db, err := a.database(ctx, in.DatabaseID)
		if err != nil {
			return err
		}
		if db.Engine != engine.PostgreSQL {
			return fmt.Errorf("physical backup is for PostgreSQL")
		}
		switch in.PhysicalMode {
		case "", "full", "full_incr":
		default:
			return fmt.Errorf("physicalMode must be full or full_incr")
		}
		if in.PhysicalMode == "" {
			in.PhysicalMode = "full"
		}
	} else if in.Method == "logical" {
		in.PhysicalMode = ""
	}
	if err := in.Schedule.Validate(); err != nil {
		return err
	}
	if err := in.Retention.Validate(); err != nil {
		return err
	}
	for _, name := range append(append(append([]string{}, in.ExcludeTables...), in.ExcludeData...), in.IncludeSchemas...) {
		if name != "" && !sqlident.Valid(name) {
			return fmt.Errorf("invalid identifier %q", name)
		}
	}
	if in.Name == "" {
		return fmt.Errorf("name is required")
	}
	if in.MaxBackupSizeMB < 0 || in.MaxTotalSizeMB < 0 {
		return fmt.Errorf("size limits cannot be negative")
	}
	if in.MaxFailedTries == 0 {
		in.MaxFailedTries = 3
	}
	return nil
}

func mergeSettings(existing, incoming json.RawMessage, secretFields []string) (json.RawMessage, error) {
	cur := map[string]any{}
	next := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cur); err != nil {
			return nil, err
		}
	}
	if len(incoming) > 0 {
		if err := json.Unmarshal(incoming, &next); err != nil {
			return nil, err
		}
	}
	secret := map[string]bool{}
	for _, name := range secretFields {
		secret[name] = true
	}
	for key, value := range next {
		if secret[key] {
			text, _ := value.(string)
			if text == "" {
				continue
			}
		}
		cur[key] = value
	}
	return json.Marshal(cur)
}
