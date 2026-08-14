//go:build example

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"

	client "github.com/machbase/neo-client/v2"
)

const tableName = "GO860_SQL_SAMPLE"

func main() {
	if err := run(context.Background()); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	db, err := sql.Open("machbase",
		"server=tcp://sys:manager@127.0.0.1:5656;statement_cache=auto")
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	if _, err := db.ExecContext(ctx, "CREATE TRANSACTION TABLE "+tableName+
		" (ID INTEGER PRIMARY KEY, AMOUNT DECIMAL(30,12), NOTE VARCHAR(80))"); err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP TABLE "+tableName) }()

	amount, err := client.ParseDecimal("123.450000000000", 30, 12)
	if err != nil {
		return fmt.Errorf("parse decimal: %w", err)
	}
	result, err := db.ExecContext(ctx,
		"INSERT INTO "+tableName+" VALUES (:id, :amount, :note)",
		sql.Named("note", "database/sql"),
		sql.Named("amount", amount),
		sql.Named("id", int32(1)))
	if err != nil {
		return fmt.Errorf("named insert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("insert rows affected: %w", err)
	}
	fmt.Printf("insert affected=%d\n", affected)
	if _, err := result.LastInsertId(); err == nil {
		return errors.New("LastInsertId unexpectedly succeeded")
	} else {
		fmt.Println("last_insert_id unsupported=true")
	}

	// database/sql returns DECIMAL as exact text. Parse with the declared shape
	// when application code needs api.Decimal operations.
	var exactText string
	if err := db.QueryRowContext(ctx,
		"SELECT AMOUNT FROM "+tableName+" WHERE ID=:id",
		sql.Named("id", int32(1))).Scan(&exactText); err != nil {
		return fmt.Errorf("query decimal: %w", err)
	}
	parsed, err := client.ParseDecimal(exactText, 30, 12)
	if err != nil {
		return fmt.Errorf("parse queried decimal: %w", err)
	}
	fmt.Printf("amount=%s precision=%d scale=%d\n",
		parsed.String(), parsed.Precision(), parsed.Scale())

	if _, err := db.ExecContext(ctx,
		"INSERT INTO "+tableName+" VALUES (?, ?, ?)", int32(2), nil, nil); err != nil {
		return fmt.Errorf("insert null: %w", err)
	}
	var nullableAmount sql.NullString
	var nullableNote sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT AMOUNT, NOTE FROM "+tableName+" WHERE ID=?", int32(2)).
		Scan(&nullableAmount, &nullableNote); err != nil {
		return fmt.Errorf("query null: %w", err)
	}
	fmt.Printf("nullable amount.valid=%t note.valid=%t\n",
		nullableAmount.Valid, nullableNote.Valid)

	// Rollback: the inserted row must disappear.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rollback example: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO "+tableName+" VALUES (?, ?, ?)", int32(10), amount, "rollback"); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert for rollback: %w", err)
	}
	if err := tx.Rollback(); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	var count int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM "+tableName+" WHERE ID=?", int32(10)).Scan(&count); err != nil {
		return fmt.Errorf("verify rollback: %w", err)
	}
	fmt.Printf("after rollback count=%d\n", count)
	if !errors.Is(tx.Commit(), sql.ErrTxDone) {
		return errors.New("finished transaction did not return sql.ErrTxDone")
	}

	// Commit: the inserted row must remain visible on a new operation.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin commit example: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO "+tableName+" VALUES (?, ?, ?)", int32(11), amount, "commit"); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert for commit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM "+tableName+" WHERE ID=?", int32(11)).Scan(&count); err != nil {
		return fmt.Errorf("verify commit: %w", err)
	}
	fmt.Printf("after commit count=%d\n", count)

	// Prepared named statement is deliberately executed twice to demonstrate reuse.
	stmt, err := db.PrepareContext(ctx,
		"SELECT NOTE FROM "+tableName+" WHERE ID=:id")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()
	for _, id := range []int32{1, 11} {
		var note sql.NullString
		if err := stmt.QueryRowContext(ctx, sql.Named("id", id)).Scan(&note); err != nil {
			return fmt.Errorf("prepared query id=%d: %w", id, err)
		}
		fmt.Printf("prepared id=%d note=%q\n", id, note.String)
	}

	rows, err := db.QueryContext(ctx, "SELECT ID, NOTE FROM "+tableName+" ORDER BY ID")
	if err != nil {
		return fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int32
		var note sql.NullString
		if err := rows.Scan(&id, &note); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		fmt.Printf("list id=%d note.valid=%t note=%q\n", id, note.Valid, note.String)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}
	return nil
}
