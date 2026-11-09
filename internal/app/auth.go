package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const operatorID = "local"

var (
	ErrPassword   = errors.New("口令不正确")
	ErrConfigured = errors.New("口令已经设置")
	ErrUnset      = errors.New("还没有设置口令")
)

type operatorRecord struct {
	PasswordHash string `json:"passwordHash"`
	// ChangedAt ends every session issued before it.
	ChangedAt time.Time `json:"changedAt"`
}

func (a *App) OperatorConfigured(ctx context.Context) (bool, error) {
	_, err := a.readOperator(ctx)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "not found") {
		return false, nil
	}
	return false, err
}

func (a *App) SetOperatorPassword(ctx context.Context, password string) error {
	if err := checkPasswordLength(password); err != nil {
		return err
	}
	configured, err := a.OperatorConfigured(ctx)
	if err != nil {
		return err
	}
	if configured {
		return ErrConfigured
	}
	if err := a.writeOperator(ctx, password); err != nil {
		return err
	}
	a.Audit(ctx, "operator.setup", "password set")
	return nil
}

// ResetOperatorPassword replaces the password from the command line and ends
// the sessions that are open in the browser.
func (a *App) ResetOperatorPassword(ctx context.Context, password string) error {
	if err := checkPasswordLength(password); err != nil {
		return err
	}
	if err := a.writeOperator(ctx, password); err != nil {
		return err
	}
	a.Audit(ctx, "operator.reset", "password reset from the command line")
	return nil
}

func (a *App) writeOperator(ctx context.Context, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.cat.Put(ctx, kindOperator, operatorID, operatorRecord{PasswordHash: string(hash), ChangedAt: time.Now().UTC()})
}

func (a *App) CheckOperatorPassword(ctx context.Context, password string) error {
	record, err := a.readOperator(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrUnset
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(password)) != nil {
		return ErrPassword
	}
	return nil
}

// PasswordChangedAt is zero when no password is set.
func (a *App) PasswordChangedAt(ctx context.Context) time.Time {
	record, err := a.readOperator(ctx)
	if err != nil {
		return time.Time{}
	}
	return record.ChangedAt
}

func (a *App) readOperator(ctx context.Context) (operatorRecord, error) {
	var record operatorRecord
	err := a.cat.Get(ctx, kindOperator, operatorID, &record)
	return record, err
}

func checkPasswordLength(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return errors.New("口令需要 8 到 72 个字符")
	}
	return nil
}
