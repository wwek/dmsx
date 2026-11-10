package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"dmsx/internal/backup"
	"dmsx/internal/engine"
	"dmsx/internal/storage"
	"dmsx/internal/streamcrypt"
)

func (a *App) preparePhysical(ctx context.Context, cfg *BackupConfig, run *BackupRun, src *engine.Source) (cleanup func(), err error) {
	cleanup = func() {}
	systemID, err := engine.PostgresSystemID(ctx, *src)
	if err != nil {
		return cleanup, err
	}
	src.SystemID = systemID
	run.SystemID = systemID

	mode := strings.TrimSpace(cfg.PhysicalMode)
	if mode == "" {
		mode = "full"
	}
	run.PhysicalKind = "FULL"

	if run.ForceFull || mode != "full_incr" {
		return cleanup, nil
	}
	parent, ok := a.latestPhysicalParent(ctx, cfg.ID)
	if !ok || parent.ManifestKey == "" {
		return cleanup, nil
	}
	path, remove, err := a.downloadManifest(ctx, parent)
	if err != nil {
		return cleanup, err
	}
	cleanup = remove
	src.IncrementalManifest = path
	run.PhysicalKind = "INCR"
	run.ParentRunID = parent.ID
	if parent.RootRunID != "" {
		run.RootRunID = parent.RootRunID
	} else {
		run.RootRunID = parent.ID
	}
	return cleanup, nil
}

func (a *App) latestPhysicalParent(ctx context.Context, configID string) (BackupRun, bool) {
	runs, err := a.ListRuns(ctx)
	if err != nil {
		return BackupRun{}, false
	}
	var candidates []BackupRun
	for _, run := range runs {
		if run.ConfigID != configID || run.Status != "success" || run.ManifestKey == "" {
			continue
		}
		if !strings.HasPrefix(run.Codec, "pg-base") {
			continue
		}
		candidates = append(candidates, run)
	}
	if len(candidates) == 0 {
		return BackupRun{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].StartedAt.After(candidates[j].StartedAt)
	})
	return candidates[0], true
}

func (a *App) downloadManifest(ctx context.Context, parent BackupRun) (string, func(), error) {
	store, _, err := a.store(ctx, parent.StorageID)
	if err != nil {
		return "", func() {}, err
	}
	body, err := store.Open(ctx, parent.ManifestKey)
	if err != nil {
		return "", func() {}, fmt.Errorf("open parent manifest: %w", err)
	}
	defer body.Close()

	reader := io.Reader(body)
	if parent.Encrypted {
		reader, err = streamcrypt.NewReader(body, a.master)
		if err != nil {
			return "", func() {}, err
		}
	}

	dir, err := os.MkdirTemp("", "dmsx-manifest-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "backup_manifest")
	out, err := os.Create(path)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := out.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func (a *App) savePhysicalResult(ctx context.Context, cfg BackupConfig, run *BackupRun, result backup.Result, store storage.Store) error {
	run.StartLSN = result.Format.StartLSN
	run.StopLSN = result.Format.StopLSN
	run.TimelineID = result.Format.TimelineID
	if run.PhysicalKind == "FULL" && run.RootRunID == "" {
		run.RootRunID = run.ID
	}
	if len(result.Format.Manifest) == 0 {
		return nil
	}
	key := run.FileName + ".manifest"
	payload := bytes.NewReader(result.Format.Manifest)
	if cfg.Encrypted {
		var sealed bytes.Buffer
		writer, err := streamcrypt.NewWriter(&sealed, a.master, manifestFileID(run.ID))
		if err != nil {
			return err
		}
		if _, err := writer.Write(result.Format.Manifest); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		payload = bytes.NewReader(sealed.Bytes())
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Minute)
	defer cancel()
	if err := store.Put(saveCtx, key, payload); err != nil {
		return fmt.Errorf("save backup manifest: %w", err)
	}
	run.ManifestKey = key
	return nil
}

func manifestFileID(runID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("dmsx-manifest:"+runID))
}
