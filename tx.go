package client

import (
	"context"
	"database/sql"
	"fmt"
)

// Tx runs fn inside a transaction on db.
//
// If fn returns nil, the transaction is committed.
// If fn returns an error, the transaction is rolled back and the error is
// returned as-is (rollback failures are attached to it).
// If fn panics, the transaction is rolled back and the panic is re-raised.
//
// machbase does not support transaction options (isolation level, read-only),
// so this helper always uses default options.
//
// Note: transactional DML is only supported on regular tables created with
// CREATE TABLE. TAG tables do not support BEGIN/COMMIT/ROLLBACK and will
// fail with MACHCLI-ERR-2362.
func Tx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	return finishTx(tx, fn)
}

// TxConn runs fn inside a transaction on a single connection.
// Use it when the caller already holds a *sql.Conn (e.g. acquired via
// db.Conn(ctx)) and needs the transaction to run on that specific connection.
// Commit/rollback/panic semantics are identical to Tx.
func TxConn(ctx context.Context, conn *sql.Conn, fn func(tx *sql.Tx) error) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	return finishTx(tx, fn)
}

// finishTx is the shared commit/rollback/panic-recovery logic for Tx and TxConn.
func finishTx(tx *sql.Tx, fn func(tx *sql.Tx) error) error {
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // re-panic after rollback
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback error: %v)", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}
