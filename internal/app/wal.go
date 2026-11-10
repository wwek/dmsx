package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"dmsx/internal/engine"
	"dmsx/internal/wal"
)

func (a *App) ensureWalStreamer(cfg BackupConfig) {
	if !cfg.WalEnabled || cfg.Method != "physical" {
		return
	}
	key := "wal:" + cfg.DatabaseID
	a.mu.Lock()
	_, running := a.cancels[key]
	a.mu.Unlock()
	if running {
		return
	}
	jobCtx, cancel := context.WithCancel(a.root)
	a.track(key, cancel)
	go a.runWalStreamer(jobCtx, cfg)
}

func (a *App) runWalStreamer(ctx context.Context, cfg BackupConfig) {
	defer a.untrack("wal:" + cfg.DatabaseID)
	for ctx.Err() == nil {
		err := a.streamWalOnce(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		return
	}
}

func (a *App) streamWalOnce(ctx context.Context, cfg BackupConfig) error {
	db, err := a.database(ctx, cfg.DatabaseID)
	if err != nil {
		return err
	}
	src, release, err := a.source(ctx, db, true)
	if err != nil {
		return err
	}
	defer release()
	passPath, cleanup, err := engine.PreparePGPass(src)
	if err != nil {
		return err
	}
	defer cleanup()

	dir := wal.WatchDir(a.dataDir, db.ID)
	uploadCtx, uploadCancel := context.WithCancel(ctx)
	defer uploadCancel()
	go a.uploadWalLoop(uploadCtx, cfg, dir)

	return wal.Receive(ctx, wal.ReceiveSpec{
		Host:     src.Host,
		Port:     src.Port,
		User:     src.User,
		Password: src.Password,
		SSL:      src.SSL,
		Slot:     wal.SlotName(db.ID),
		Dir:      dir,
		EnvPath:  passPath,
	})
}

func (a *App) uploadWalLoop(ctx context.Context, cfg BackupConfig, dir string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = a.uploadReadyWal(ctx, cfg, dir)
		}
	}
}

func (a *App) uploadReadyWal(ctx context.Context, cfg BackupConfig, dir string) error {
	store, _, err := a.store(ctx, cfg.StorageID)
	if err != nil {
		return err
	}
	segments, err := wal.List(dir, wal.DefaultSegmentSize)
	if err != nil {
		return err
	}
	for _, seg := range segments {
		if !seg.Complete {
			continue
		}
		if a.walAlreadyUploaded(ctx, cfg.DatabaseID, seg.Name) {
			_ = os.Remove(seg.Path)
			continue
		}
		key := fmt.Sprintf("wal/%s/%s", cfg.DatabaseID, seg.Name)
		f, err := os.Open(seg.Path)
		if err != nil {
			return err
		}
		putErr := store.Put(ctx, key, f)
		_ = f.Close()
		if putErr != nil {
			return putErr
		}
		row := WalSegment{
			ID:         uuid.NewString(),
			DatabaseID: cfg.DatabaseID,
			StorageID:  cfg.StorageID,
			Name:       seg.Name,
			ObjectKey:  key,
			Bytes:      seg.Size,
			TimelineID: seg.TimelineID,
			UploadedAt: time.Now().UTC(),
		}
		if err := a.repo.Put(ctx, kindWalSegment, row.ID, row); err != nil {
			return err
		}
		_ = os.Remove(seg.Path)
	}
	return nil
}

func (a *App) walAlreadyUploaded(ctx context.Context, databaseID, name string) bool {
	rows, err := a.ListWalSegments(ctx, databaseID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.Name == name {
			return true
		}
	}
	return false
}

func (a *App) ListWalSegments(ctx context.Context, databaseID string) ([]WalSegment, error) {
	var rows []WalSegment
	if err := a.repo.List(ctx, kindWalSegment, &rows); err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, row := range rows {
		if databaseID == "" || row.DatabaseID == databaseID {
			out = append(out, row)
		}
	}
	if out == nil {
		out = []WalSegment{}
	}
	return out, nil
}

// WritePITRGuide writes a short restore script next to downloaded artifacts.
func WritePITRGuide(dir, recoveryTarget string) error {
	return os.WriteFile(filepath.Join(dir, "dmsx-pitr.txt"), []byte(pitrGuideText("", recoveryTarget)), 0o600)
}

// PITRGuide returns plain-text restore steps for one database's physical chain.
func (a *App) PITRGuide(ctx context.Context, databaseID string) (string, error) {
	if databaseID == "" {
		return "", fmt.Errorf("databaseId is required")
	}
	if _, err := a.database(ctx, databaseID); err != nil {
		return "", err
	}
	runs, err := a.ListRuns(ctx)
	if err != nil {
		return "", err
	}
	var latest *BackupRun
	for i := range runs {
		run := &runs[i]
		if run.DatabaseID != databaseID || run.Status != "success" || !strings.HasPrefix(run.Codec, "pg-base") {
			continue
		}
		if latest == nil || run.StartedAt.After(latest.StartedAt) {
			latest = run
		}
	}
	target := time.Now().UTC().Format(time.RFC3339)
	if latest != nil && latest.StopLSN != "" {
		target = latest.StopLSN
	}
	return pitrGuideText(databaseID, target), nil
}

func pitrGuideText(databaseID, recoveryTarget string) string {
	walPath := "wal/<databaseId>/"
	if databaseID != "" {
		walPath = "wal/" + databaseID + "/"
	}
	return `# dmsx physical restore outline
#
# 1. Download the latest successful FULL base backup tar (and any INCR tars
#    after it). Combine incrementals with pg_combinebackup when needed.
# 2. Extract into an empty PGDATA directory.
# 3. Copy archived WAL segments from ` + walPath + ` into pg_wal/ or point
#    restore_command at that prefix.
# 4. Create recovery.signal and set recovery_target_time / recovery_target_lsn.
#
# Suggested target: ` + recoveryTarget + `
`
}
