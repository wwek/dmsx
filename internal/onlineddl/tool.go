package onlineddl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveTool(bin string) (string, error) {
	if env := os.Getenv("DMSX_" + strings.ToUpper(strings.ReplaceAll(bin, "-", "_"))); env != "" {
		return env, nil
	}
	if bin == "gh-ost" {
		if path, err := bundledGhOst(); err == nil {
			return path, nil
		}
		if path, err := besideExecutable("gh-ost"); err == nil {
			return path, nil
		}
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%s was not found. A packaged dmsx build includes gh-ost; pt-online-schema-change is installed separately", bin)
	}
	return path, nil
}

func besideExecutable(name string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(exe), name)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", path)
	}
	return path, nil
}
