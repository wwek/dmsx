// Package sqlident checks names and predicates before they are interpolated
// into a statement. Bind parameters cannot bind a table name or a WHERE clause,
// so the callers that build those statements come through here first.
package sqlident

import (
	"fmt"
	"strings"
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

// ValidatePredicate accepts the filter an operator typed for an archive or a
// cold query. It is not a SQL parser. It rejects statement breaks, comments,
// and the verbs that would turn the filter into a second statement or a file
// read. The database user is still the privilege boundary.
func ValidatePredicate(predicate string) error {
	text := strings.TrimSpace(predicate)
	if text == "" {
		return fmt.Errorf("filter is empty")
	}
	if len(text) > 4000 {
		return fmt.Errorf("filter is longer than 4000 characters")
	}
	lower := strings.ToLower(text)
	banned := []string{
		";", "--", "/*", "*/", "#",
		"read_", "copy ", "attach", "install", "load ", "pragma",
		"set ", "call ", " into ", "outfile", "dumpfile", "sleep(", "benchmark(",
		"union", "information_schema", "mysql.", "performance_schema",
	}
	for _, word := range banned {
		if strings.Contains(lower, word) {
			return fmt.Errorf("filter contains %q, which is not allowed", word)
		}
	}
	return nil
}
