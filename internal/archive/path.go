package archive

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dmsx/internal/sqlident"
)

// ObjectKey is qiniu-bucket/dms_archives/<database>/<table>/<year>_archive.parquet.
// A later run in the same year adds _<unix> so the previous object is kept.
// Cold queries read every object with *.parquet.
func ObjectKey(database, table string, year int, stamp string) (string, error) {
	if err := names(database, table); err != nil {
		return "", err
	}
	if year < 1970 || year > 2100 {
		return "", fmt.Errorf("year must be between 1970 and 2100")
	}
	name := strconv.Itoa(year) + "_archive"
	if stamp != "" {
		if strings.Contains(stamp, "/") || strings.Contains(stamp, ".") {
			return "", fmt.Errorf("archive suffix is not a single path segment")
		}
		name += "_" + stamp
	}
	return fmt.Sprintf("dms_archives/%s/%s/%s.parquet", database, table, name), nil
}

func Glob(database, table string) (string, error) {
	if err := names(database, table); err != nil {
		return "", err
	}
	return fmt.Sprintf("dms_archives/%s/%s/*.parquet", database, table), nil
}

func Stamp() string { return strconv.FormatInt(time.Now().UTC().Unix(), 10) }

func names(database, table string) error {
	if !sqlident.Valid(database) || !sqlident.Valid(table) {
		return fmt.Errorf("database and table must be plain identifiers")
	}
	return nil
}
