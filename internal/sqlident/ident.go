// Package sqlident checks names and predicates before they are interpolated
// into a statement. Bind parameters cannot bind a table name or a WHERE clause,
// so the callers that build those statements come through here first.
package sqlident

import (
	"fmt"
	"unicode"
)

func Valid(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			if i == 0 && unicode.IsDigit(r) {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func QuoteMySQL(name string) (string, error) {
	if !Valid(name) {
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	return "`" + name + "`", nil
}
