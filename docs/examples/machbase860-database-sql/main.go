package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/machbase/neo-client"
	"github.com/machbase/neo-client/api"
)

const tableName = "GO860_SQL_SAMPLE"

func main() {
	ctx := context.Background()
	db, err := sql.Open("machbase",
		"server=tcp://sys:manager@127.0.0.1:5656;statement_cache=auto")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}

	_, _ = db.ExecContext(ctx, "DROP TABLE "+tableName)
	_, err = db.ExecContext(ctx, "CREATE TRANSACTION TABLE "+tableName+
		" (ID INTEGER PRIMARY KEY, AMOUNT DECIMAL(30,12), NOTE VARCHAR(80))")
	if err != nil {
		log.Fatal(err)
	}
	defer db.ExecContext(ctx, "DROP TABLE "+tableName)

	amount, err := api.ParseDecimal("123.450000000000", 30, 12)
	if err != nil {
		log.Fatal(err)
	}
	result, err := db.ExecContext(ctx,
		"INSERT INTO "+tableName+" VALUES (:id, :amount, :note)",
		sql.Named("note", "database/sql"),
		sql.Named("amount", amount),
		sql.Named("id", int32(1)))
	if err != nil {
		log.Fatal(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("insert affected=%d\n", affected)

	var exactText string
	err = db.QueryRowContext(ctx,
		"SELECT AMOUNT FROM "+tableName+" WHERE ID=:id",
		sql.Named("id", int32(1))).Scan(&exactText)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("amount=%s\n", exactText)

	_, err = db.ExecContext(ctx,
		"INSERT INTO "+tableName+" VALUES (?, ?, ?)", int32(2), nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	var nullableAmount sql.NullString
	var nullableNote sql.NullString
	err = db.QueryRowContext(ctx,
		"SELECT AMOUNT, NOTE FROM "+tableName+" WHERE ID=?", int32(2)).
		Scan(&nullableAmount, &nullableNote)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("nullable amount.valid=%t note.valid=%t\n",
		nullableAmount.Valid, nullableNote.Valid)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE "+tableName+" SET NOTE=? WHERE ID=?", "rolled back", int32(1)); err != nil {
		_ = tx.Rollback()
		log.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		log.Fatal(err)
	}

	stmt, err := db.PrepareContext(ctx,
		"SELECT NOTE FROM "+tableName+" WHERE ID=:id")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()
	var note string
	if err := stmt.QueryRowContext(ctx, sql.Named("id", int32(1))).Scan(&note); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("note after rollback=%s\n", note)
}
