// Package onlineddl shells out to gh-ost, or pt-online-schema-change when asked.
// The tools create the ghost table, copy in chunks, tail the binlog and rename.
// This package parses their stdout into a progress event and does not reimplement
// the binlog applier.
package onlineddl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Request struct {
	Host      string
	Port      int
	User      string
	Password  string
	Database  string
	Statement string
	Tool      string
	Execute   bool
	ChunkSize int
}

type Progress struct {
	Copied  int64   `json:"copied"`
	Total   int64   `json:"total"`
	Percent float64 `json:"percent"`
	ETA     string  `json:"eta"`
	Line    string  `json:"line"`
}

type Event struct {
	Kind     string   `json:"kind"`
	Line     string   `json:"line,omitempty"`
	Progress Progress `json:"progress,omitempty"`
	Status   string   `json:"status,omitempty"`
	Error    string   `json:"error,omitempty"`
}

var (
	alterPattern = regexp.MustCompile(`(?is)^\s*alter\s+table\s+(?:` + "`?" + `([A-Za-z0-9_]+)` + "`?" + `\s*\.\s*)?` + "`?" + `([A-Za-z0-9_]+)` + "`?" + `\s+(.+)$`)
	copyPattern  = regexp.MustCompile(`Copy:\s*(\d+)\s*/\s*(\d+)\s+([\d.]+)%`)
	etaPattern   = regexp.MustCompile(`ETA:\s*(\S+)`)
	ptPattern    = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)%\s+(\S+)\s+remain`)
)

func ParseAlter(statement string) (database, table, body string, err error) {
	if strings.Contains(statement, ";") {
		return "", "", "", fmt.Errorf("submit one ALTER TABLE statement")
	}
	match := alterPattern.FindStringSubmatch(strings.TrimSpace(statement))
	if match == nil {
		return "", "", "", fmt.Errorf("expected ALTER TABLE <table> ...")
	}
	body = strings.TrimSpace(match[3])
	if body == "" {
		return "", "", "", fmt.Errorf("ALTER TABLE is missing its change")
	}
	return match[1], match[2], body, nil
}

func ParseLine(line string) (Progress, bool) {
	if match := copyPattern.FindStringSubmatch(line); match != nil {
		copied, _ := strconv.ParseInt(match[1], 10, 64)
		total, _ := strconv.ParseInt(match[2], 10, 64)
		percent, _ := strconv.ParseFloat(match[3], 64)
		eta := ""
		if etaMatch := etaPattern.FindStringSubmatch(line); etaMatch != nil {
			eta = etaMatch[1]
		}
		return Progress{Copied: copied, Total: total, Percent: percent, ETA: eta, Line: line}, true
	}
	if match := ptPattern.FindStringSubmatch(line); match != nil {
		percent, _ := strconv.ParseFloat(match[1], 64)
		return Progress{Percent: percent, ETA: match[2], Line: line}, true
	}
	return Progress{}, false
}

// Run starts the tool and calls onLine for every stdout/stderr line. The
// password is written to a mode-600 defaults file and is not put in argv.
func Run(ctx context.Context, req Request, onLine func(string)) error {
	database, table, body, err := ParseAlter(req.Statement)
	if err != nil {
		return err
	}
	if database == "" {
		database = req.Database
	}
	if req.ChunkSize <= 0 {
		req.ChunkSize = 1000
	}
	tool := req.Tool
	if tool == "" {
		tool = "gh-ost"
	}
	cnf, err := writeConf(req)
	if err != nil {
		return err
	}
	defer os.RemoveAll(cnf.dir)

	var bin string
	var args []string
	switch tool {
	case "pt-osc", "pt-online-schema-change":
		bin = "pt-online-schema-change"
		args = []string{
			"--alter", body,
			"--chunk-size", strconv.Itoa(req.ChunkSize),
			"--progress", "time,5",
			"--nocheck-replication-filters",
			"--recursion-method=none",
			fmt.Sprintf("--defaults-file=%s", cnf.path),
			fmt.Sprintf("h=%s,P=%d,D=%s,t=%s", req.Host, req.Port, database, table),
		}
		if req.Execute {
			args = append([]string{"--execute"}, args...)
		} else {
			args = append([]string{"--dry-run"}, args...)
		}
	default:
		bin = "gh-ost"
		dir, err := os.MkdirTemp("", "dmsx-ghost-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		args = []string{
			"--conf=" + cnf.path,
			"--host=" + req.Host,
			"--port=" + strconv.Itoa(req.Port),
			"--database=" + database,
			"--table=" + table,
			"--alter=" + body,
			"--chunk-size=" + strconv.Itoa(req.ChunkSize),
			"--exact-rowcount",
			"--concurrent-rowcount",
			"--allow-on-master",
			"--assume-rbr",
			"--initially-drop-ghost-table",
			"--initially-drop-old-table",
			"--serve-socket-file=" + filepath.Join(dir, "ghost.sock"),
			"--panic-flag-file=" + filepath.Join(dir, "ghost.panic"),
			"--verbose",
		}
		if req.Execute {
			args = append(args, "--execute")
		}
	}
	path, err := resolveTool(bin)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	scan := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
	}
	go scan(stdout)
	go scan(stderr)
	wg.Wait()
	return cmd.Wait()
}

type confFile struct{ dir, path string }

func writeConf(req Request) (*confFile, error) {
	dir, err := os.MkdirTemp("", "dmsx-ddl-*")
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)
	password := strings.ReplaceAll(req.Password, `\`, `\\`)
	password = strings.ReplaceAll(password, `"`, `\"`)
	body := fmt.Sprintf("[client]\nuser=%s\npassword=\"%s\"\nhost=%s\nport=%d\n", req.User, password, req.Host, req.Port)
	path := filepath.Join(dir, "client.cnf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &confFile{dir: dir, path: path}, nil
}
