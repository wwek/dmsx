package app

import (
	"context"
	"fmt"
	"strings"

	"dmsx/internal/engine"
)

func verifyRestore(ctx context.Context, src engine.Source) (string, error) {
	switch src.Engine {
	case engine.PostgreSQL:
		out, err := engine.QueryPostgres(ctx, src, "SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog','information_schema')")
		if err != nil {
			return "", err
		}
		return "pg_tables=" + strings.TrimSpace(out), nil
	case engine.MySQL, engine.MariaDB:
		out, err := engine.QueryMySQL(ctx, src, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()")
		if err != nil {
			return "", err
		}
		return "tables=" + strings.TrimSpace(out), nil
	case engine.MongoDB:
		return "mongo restore completed", nil
	default:
		return "", fmt.Errorf("verify is not implemented for %s", src.Engine)
	}
}
