package engine

import "testing"

func TestParseBaseBackupLSN(t *testing.T) {
	stderr := `
pg_basebackup: initiating base backup, waiting for checkpoint to complete
pg_basebackup: checkpoint completed
pg_basebackup: write-ahead log start point: 0/3000028 on timeline 1
pg_basebackup: write-ahead log end point: 0/3000130
pg_basebackup: base backup completed
`
	got := ParseBaseBackupLSN(stderr)
	if got.StartLSN != "0/3000028" || got.StopLSN != "0/3000130" || got.TimelineID != 1 {
		t.Fatalf("%+v", got)
	}
}
