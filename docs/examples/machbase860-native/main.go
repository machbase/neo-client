package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

const tableName = "GO860_NATIVE_SAMPLE"

func main() {
	ctx := context.Background()
	database, err := machgo.NewDatabase(&machgo.Config{Host: "127.0.0.1", Port: 5656})
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	conn, err := database.Connect(ctx, api.WithPassword("sys", "manager"))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	_ = conn.Exec(ctx, "DROP TABLE "+tableName).Err()
	mustExec(ctx, conn, "CREATE TRANSACTION TABLE "+tableName+
		" (ID INTEGER PRIMARY KEY, AMOUNT DECIMAL(30,12), NOTE VARCHAR(80))")
	defer conn.Exec(ctx, "DROP TABLE "+tableName)

	amount, err := api.ParseDecimal("123456789012345678.901234567890", 30, 12)
	if err != nil {
		log.Fatal(err)
	}

	result := conn.Exec(ctx,
		"INSERT INTO "+tableName+" VALUES (:id, :amount, :note)",
		api.Named("note", "named insert"),
		api.Named("amount", amount),
		api.Named("id", int32(1)))
	if err := result.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("insert affected=%d\n", result.RowsAffected())

	var id int32
	var got api.Decimal
	var note string
	err = conn.QueryRow(ctx,
		"SELECT ID, AMOUNT, NOTE FROM "+tableName+" WHERE ID=:id OR ID=:id",
		api.Named("id", int32(1))).Scan(&id, &got, &note)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("row id=%d amount=%s precision=%d scale=%d note=%s\n",
		id, got.String(), got.Precision(), got.Scale(), note)

	mustExec(ctx, conn, "INSERT INTO "+tableName+" VALUES (?, ?, ?)", int32(2), nil, nil)
	var nullableAmount sql.Null[api.Decimal]
	var nullableNote sql.NullString
	err = conn.QueryRow(ctx,
		"SELECT AMOUNT, NOTE FROM "+tableName+" WHERE ID=?", int32(2)).
		Scan(&nullableAmount, &nullableNote)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("nullable amount.valid=%t note.valid=%t\n",
		nullableAmount.Valid, nullableNote.Valid)

	appender, err := conn.Appender(ctx, tableName, api.WithAppenderBuffer(1))
	if err != nil {
		log.Fatal(err)
	}
	if appender.TableType() != api.TableTypeTransaction {
		log.Fatalf("unexpected table type: %s", appender.TableType())
	}
	if err := appender.Append(int32(3), amount, "appender"); err != nil {
		log.Fatal(err)
	}
	success, failed, err := appender.Close()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("append success=%d failed=%d\n", success, failed)

	result = conn.Exec(ctx,
		"UPDATE "+tableName+" SET NOTE=? WHERE ID=?", "updated", int32(3))
	if err := result.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("update affected=%d\n", result.RowsAffected())
}

func mustExec(ctx context.Context, conn api.Conn, query string, args ...any) {
	if err := conn.Exec(ctx, query, args...).Err(); err != nil {
		log.Fatal(err)
	}
}
