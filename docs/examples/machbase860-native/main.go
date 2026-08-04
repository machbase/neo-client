package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

const tableName = "GO860_NATIVE_SAMPLE"

func main() {
	if err := run(context.Background()); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	database, err := machgo.NewDatabase(&machgo.Config{Host: "127.0.0.1", Port: 5656})
	if err != nil {
		return fmt.Errorf("create database handle: %w", err)
	}
	defer database.Close()

	conn, err := database.Connect(ctx, api.WithPassword("sys", "manager"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	if err := exec(ctx, conn, "CREATE TRANSACTION TABLE "+tableName+
		" (ID INTEGER PRIMARY KEY, AMOUNT DECIMAL(30,12), NOTE VARCHAR(80))"); err != nil {
		return err
	}
	defer func() { _ = conn.Exec(context.Background(), "DROP TABLE "+tableName).Err() }()

	amount, err := api.ParseDecimal("123456789012345678.901234567890", 30, 12)
	if err != nil {
		return fmt.Errorf("parse decimal: %w", err)
	}

	// Named arguments may be supplied in an order different from SQL markers.
	result := conn.Exec(ctx,
		"INSERT INTO "+tableName+" VALUES (:id, :amount, :note)",
		api.Named("note", "named insert"),
		api.Named("amount", amount),
		api.Named("id", int32(1)))
	if err := result.Err(); err != nil {
		return fmt.Errorf("named insert: %w", err)
	}
	fmt.Printf("insert affected=%d\n", result.RowsAffected())

	// A repeated :id marker consumes one supplied named value.
	var id int32
	var got api.Decimal
	var note string
	if err := conn.QueryRow(ctx,
		"SELECT ID, AMOUNT, NOTE FROM "+tableName+" WHERE ID=:id OR ID=:id",
		api.Named("id", int32(1))).Scan(&id, &got, &note); err != nil {
		return fmt.Errorf("query decimal: %w", err)
	}
	fmt.Printf("row id=%d amount=%s precision=%d scale=%d note=%s\n",
		id, got.String(), got.Precision(), got.Scale(), note)

	if err := exec(ctx, conn, "INSERT INTO "+tableName+" VALUES (?, ?, ?)", int32(2), nil, nil); err != nil {
		return err
	}
	var nullableAmount sql.Null[api.Decimal]
	var nullableNote sql.NullString
	if err := conn.QueryRow(ctx,
		"SELECT AMOUNT, NOTE FROM "+tableName+" WHERE ID=?", int32(2)).
		Scan(&nullableAmount, &nullableNote); err != nil {
		return fmt.Errorf("query null: %w", err)
	}
	fmt.Printf("nullable amount.valid=%t note.valid=%t\n",
		nullableAmount.Valid, nullableNote.Valid)

	appender, err := conn.Appender(ctx, tableName)
	if err != nil {
		return fmt.Errorf("open appender: %w", err)
	}
	appender = appender.WithBatchMaxRows(2)
	appenderClosed := false
	defer func() {
		if !appenderClosed {
			_, _, _ = appender.Close()
		}
	}()
	if appender.TableType() != api.TableTypeTransaction {
		return fmt.Errorf("unexpected table type: %s", appender.TableType())
	}
	appendErr := appender.Append(int32(3), amount, "appender")
	if appendErr != nil {
		success, failed, closeErr := appender.Close()
		appenderClosed = true
		return fmt.Errorf("append/close: success=%d failed=%d: %w",
			success, failed, errors.Join(appendErr, closeErr))
	}
	success, failed, closeErr := appender.Close()
	appenderClosed = true
	if closeErr != nil {
		return fmt.Errorf("close appender: success=%d failed=%d: %w", success, failed, closeErr)
	}
	if success != 1 || failed != 0 {
		return fmt.Errorf("unexpected append counts: success=%d failed=%d", success, failed)
	}
	fmt.Printf("table_type=%s(%d)\n", appender.TableType(), int(appender.TableType()))
	fmt.Printf("append success=%d failed=%d\n", success, failed)

	result = conn.Exec(ctx,
		"UPDATE "+tableName+" SET NOTE=? WHERE ID>=?", "updated", int32(2))
	if err := result.Err(); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	fmt.Printf("update matched=%d\n", result.RowsAffected())

	rows, err := conn.Query(ctx, "SELECT ID, NOTE FROM "+tableName+" ORDER BY ID")
	if err != nil {
		return fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rowID int32
		var rowNote sql.NullString
		if err := rows.Scan(&rowID, &rowNote); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		fmt.Printf("list id=%d note.valid=%t note=%q\n", rowID, rowNote.Valid, rowNote.String)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}
	return nil
}

func exec(ctx context.Context, conn api.Conn, query string, args ...any) error {
	if err := conn.Exec(ctx, query, args...).Err(); err != nil {
		return fmt.Errorf("execute %q: %w", query, err)
	}
	return nil
}
