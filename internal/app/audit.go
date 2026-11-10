package app

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
)

const keepAudit = 5000

// Audit records an operator action. detail names what was touched and never
// carries a password or a storage secret.
func (a *App) Audit(ctx context.Context, action, detail string) {
	ctx = context.WithoutCancel(ctx)
	entry := AuditEntry{ID: uuid.NewString(), At: time.Now().UTC(), Action: action, Detail: detail}
	if err := a.repo.Put(ctx, kindAudit, entry.ID, entry); err != nil {
		log.Printf("audit %s: %v", action, err)
		return
	}
	_ = a.repo.Prune(ctx, kindAudit, keepAudit)
}

// ListAudit returns the newest entries first.
func (a *App) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	var rows []AuditEntry
	if err := a.repo.List(ctx, kindAudit, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []AuditEntry{}
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}
