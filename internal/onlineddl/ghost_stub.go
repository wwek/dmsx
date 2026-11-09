//go:build !ghostembed

package onlineddl

import "os"

func bundledGhOst() (string, error) {
	return "", os.ErrNotExist
}
