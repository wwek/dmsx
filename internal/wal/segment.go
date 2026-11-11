// Package wal helps archive PostgreSQL WAL for physical backup chains.
//
// Completed segments may sit briefly on local disk before upload. That local
// queue is the only staging exception for this path; the base-backup body still
// never lands as a staging file.
package wal

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const DefaultSegmentSize = 16 << 20

var walName = regexp.MustCompile(`^([0-9A-F]{8})([0-9A-F]{8})([0-9A-F]{8})$`)

type Segment struct {
	Name       string
	Path       string
	TimelineID int
	Complete   bool
	Size       int64
}

func IsPartial(name string) bool { return strings.HasSuffix(name, ".partial") }
func IsHistory(name string) bool { return strings.HasSuffix(name, ".history") }
func IsComplete(path string, size int64) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() == size
}

func List(dir string, segmentSize int64) ([]Segment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if segmentSize <= 0 {
		segmentSize = DefaultSegmentSize
	}
	var out []Segment
	for _, entry := range entries {
		if entry.IsDir() || IsPartial(entry.Name()) {
			continue
		}
		timeline, ok := parseTimeline(entry.Name())
		if !ok && !IsHistory(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, Segment{
			Name:       entry.Name(),
			Path:       path,
			TimelineID: timeline,
			Complete:   IsHistory(entry.Name()) || info.Size() == segmentSize,
			Size:       info.Size(),
		})
	}
	return out, nil
}

func parseTimeline(name string) (int, bool) {
	match := walName.FindStringSubmatch(name)
	if len(match) != 4 {
		return 0, false
	}
	n, err := strconv.ParseUint(match[1], 16, 32)
	if err != nil {
		return 0, false
	}
	return int(n), true
}
