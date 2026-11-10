package archive

import (
	"fmt"
	"strings"

	"dmsx/internal/sqlident"
)

// PrimaryKey is the ordered list of PRIMARY KEY column names.
type PrimaryKey []string

func (pk PrimaryKey) validate() error {
	if len(pk) == 0 {
		return fmt.Errorf("the table has no PRIMARY KEY; archive requires one so each batch can be deleted exactly")
	}
	for _, name := range pk {
		if !sqlident.Valid(name) {
			return fmt.Errorf("primary key column %q is not a plain identifier", name)
		}
	}
	return nil
}

func (pk PrimaryKey) quoted() ([]string, error) {
	out := make([]string, len(pk))
	for i, name := range pk {
		q, err := sqlident.QuoteMySQL(name)
		if err != nil {
			return nil, err
		}
		out[i] = q
	}
	return out, nil
}

func (pk PrimaryKey) orderBy() (string, error) {
	cols, err := pk.quoted()
	if err != nil {
		return "", err
	}
	return strings.Join(cols, ", "), nil
}

func (pk PrimaryKey) afterClause() (string, error) {
	cols, err := pk.quoted()
	if err != nil {
		return "", err
	}
	placeholders := make([]string, len(pk))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return "(" + strings.Join(cols, ", ") + ") > (" + strings.Join(placeholders, ", ") + ")", nil
}

func (pk PrimaryKey) extract(row []any, colIndex map[string]int) ([]any, error) {
	keys := make([]any, len(pk))
	for i, name := range pk {
		idx, ok := colIndex[name]
		if !ok {
			return nil, fmt.Errorf("primary key column %q is missing from the result", name)
		}
		if idx >= len(row) {
			return nil, fmt.Errorf("primary key column %q is out of range", name)
		}
		keys[i] = row[idx]
	}
	return keys, nil
}
