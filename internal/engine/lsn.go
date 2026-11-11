package engine

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	lsnStart = regexp.MustCompile(`(?i)write-ahead log start point:\s*([0-9A-F]+/[0-9A-F]+)`)
	lsnStop  = regexp.MustCompile(`(?i)write-ahead log end point:\s*([0-9A-F]+/[0-9A-F]+)`)
	lsnTL    = regexp.MustCompile(`(?i)write-ahead log start point:.*(?:\(|\bon\s+)timeline\s+(\d+)`)
)

// PhysicalLSN is parsed from pg_basebackup verbose stderr.
type PhysicalLSN struct {
	StartLSN   string
	StopLSN    string
	TimelineID int
}

func ParseBaseBackupLSN(stderr string) PhysicalLSN {
	var out PhysicalLSN
	if match := lsnStart.FindStringSubmatch(stderr); len(match) == 2 {
		out.StartLSN = strings.ToUpper(match[1])
	}
	if match := lsnStop.FindStringSubmatch(stderr); len(match) == 2 {
		out.StopLSN = strings.ToUpper(match[1])
	}
	if match := lsnTL.FindStringSubmatch(stderr); len(match) == 2 {
		out.TimelineID, _ = strconv.Atoi(match[1])
	}
	return out
}
