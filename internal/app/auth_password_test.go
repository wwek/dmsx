package app

import (
	"context"
	"testing"
)

func TestChangeOperatorPassword(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()
	if err := application.SetOperatorPassword(ctx, "old-password"); err != nil {
		t.Fatal(err)
	}
	if err := application.ChangeOperatorPassword(ctx, "wrong", "new-password"); err == nil {
		t.Fatal("expected wrong current password to fail")
	}
	if err := application.ChangeOperatorPassword(ctx, "old-password", "new-password"); err != nil {
		t.Fatal(err)
	}
	if err := application.CheckOperatorPassword(ctx, "new-password"); err != nil {
		t.Fatal(err)
	}
}
