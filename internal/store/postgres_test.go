package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type rollbackFunc func(context.Context) error

func (f rollbackFunc) Rollback(ctx context.Context) error { return f(ctx) }

func TestRollbackTransactionPreservesPrimaryError(t *testing.T) {
	primaryErr := errors.New("write failed")
	rollbackErr := errors.New("rollback failed")
	err := error(primaryErr)

	rollbackTransaction(context.Background(), rollbackFunc(func(context.Context) error {
		return rollbackErr
	}), &err)

	if !errors.Is(err, primaryErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("rollbackTransaction() error = %v, want primary and rollback errors", err)
	}
}

func TestRollbackTransactionIgnoresClosedTransaction(t *testing.T) {
	var err error
	rollbackTransaction(context.Background(), rollbackFunc(func(context.Context) error {
		return pgx.ErrTxClosed
	}), &err)
	if err != nil {
		t.Fatalf("rollbackTransaction() error = %v, want nil", err)
	}
}
