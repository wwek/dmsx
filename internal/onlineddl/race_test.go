package onlineddl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The tool writes to its standard output and to its error output, and both
// carry lines the caller has to keep. The lines have to reach the caller one
// after the other, because the caller writes them into one buffer without a
// lock.
func TestRunHandsTheLinesOverOneAfterAnother(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "fake-ghost")
	script := `#!/bin/sh
i=0
while [ $i -lt 100 ]; do
	echo "out $i"
	echo "err $i" >&2
	i=$((i+1))
done
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DMSX_GH_OST", tool)

	var active int32
	var overlapped int32
	// The buffer is written without a lock, the way the caller writes it.
	var log strings.Builder
	err := Run(context.Background(), Request{
		Host: "127.0.0.1", Port: 3306, User: "root", Password: "secret",
		Database: "shop", Statement: "ALTER TABLE orders ADD COLUMN extra varchar(32)",
	}, func(line string) {
		if atomic.AddInt32(&active, 1) > 1 {
			atomic.StoreInt32(&overlapped, 1)
		}
		log.WriteString(line)
		log.WriteByte('\n')
		time.Sleep(50 * time.Microsecond)
		atomic.AddInt32(&active, -1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&overlapped) != 0 {
		t.Fatal("the callback ran twice at the same time")
	}
	if lines := strings.Count(log.String(), "\n"); lines != 200 {
		t.Fatalf("%d lines, want 200", lines)
	}
}
