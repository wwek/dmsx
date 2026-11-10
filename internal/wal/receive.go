package wal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReceiveSpec runs pg_receivewal into Dir until ctx ends.
type ReceiveSpec struct {
	Host     string
	Port     int
	User     string
	Password string
	SSL      bool
	Slot     string
	Dir      string
	EnvPath  string // optional PGPASSFILE already prepared by the caller
}

// Receive starts pg_receivewal and waits until it exits. The caller owns Dir.
func Receive(ctx context.Context, spec ReceiveSpec) error {
	if err := os.MkdirAll(spec.Dir, 0o750); err != nil {
		return err
	}
	path, err := look("pg_receivewal")
	if err != nil {
		return err
	}
	args := []string{
		"--no-password",
		"--verbose",
		"--synchronous",
		"-D", spec.Dir,
		"-h", spec.Host,
		"-p", strconv.Itoa(spec.Port),
		"-U", spec.User,
	}
	if spec.Slot != "" {
		args = append(args, "--slot="+spec.Slot, "--create-slot", "--if-not-exists")
	}
	cmd := exec.CommandContext(ctx, path, args...)
	env := append(os.Environ(), "PGPASSFILE="+spec.EnvPath)
	if spec.SSL {
		env = append(env, "PGSSLMODE=require")
	} else {
		env = append(env, "PGSSLMODE=disable")
	}
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("pg_receivewal: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func look(name string) (string, error) {
	env := "DMSX_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if value := os.Getenv(env); value != "" {
		return value, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s was not found; set %s or install the client tools", name, env)
	}
	return path, nil
}

// SlotName builds a stable replication slot name for a database id.
func SlotName(databaseID string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, databaseID)
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return "dmsx_wal_" + clean
}

// WatchDir is the local queue path under dataDir for one database.
func WatchDir(dataDir, databaseID string) string {
	return filepath.Join(dataDir, "wal", databaseID)
}
