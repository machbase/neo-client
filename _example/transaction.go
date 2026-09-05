//go:build example

package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	client "github.com/machbase/neo-client/v2"
)

var server = "127.0.0.1:5656"
var user = "sys"
var password = "manager"

func main() {
	flag.StringVar(&server, "s", server, "server address")
	flag.StringVar(&user, "u", user, "user")
	flag.StringVar(&password, "p", password, "password")
	flag.Parse()

	host, port, err := net.SplitHostPort(server)
	if err != nil {
		panic(err)
	}

	fields := []string{}
	fields = append(fields, fmt.Sprintf("server=tcp://%s:%s@%s:%s", user, password, host, port))
	// other options
	// fields = append(fields, "fetch_rows=777")
	// fields = append(fields, "statement_cache=off")
	// fields = append(fields, "io_metrics=true")
	// fields = append(fields, "alternative_servers=127.0.0.2:5656")

	db, err := sql.Open("machbase", strings.Join(fields, ";"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	// NOTE: Transaction tables do support transactions (BEGIN/COMMIT/ROLLBACK).
	// Use a transaction table created with CREATE TABLE for transactional DML.
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS EXAMPLE_TX (
		NAME   VARCHAR(100),
		TIME   DATETIME,
		VALUE  DOUBLE
	)`)
	if err != nil {
		panic(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM EXAMPLE_TX`); err != nil {
		panic(err)
	}

	insert := func(tx *sql.Tx, name string, count int) error {
		ts := time.Now()
		for i := 0; i < count; i++ {
			// NAME, TIME, VALUE
			rec := []any{
				name,
				ts.Add(time.Second * time.Duration(i)),
				3.14 * float64(i),
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO EXAMPLE_TX VALUES (?, ?, ?)`, rec...); err != nil {
				return err
			}
		}
		return nil
	}

	count := func() int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM EXAMPLE_TX`).Scan(&n); err != nil {
			panic(err)
		}
		return n
	}

	// 1) INSERT then ROLLBACK: inserted rows are discarded.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		panic(err)
	}
	if err := insert(tx, "rollback-insert", 5); err != nil {
		panic(err)
	}
	if err := tx.Rollback(); err != nil {
		panic(err)
	}
	fmt.Println("after rollback of insert, rows:", count()) // expect 0

	// 2) INSERT then COMMIT: inserted rows are persisted.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		panic(err)
	}
	if err := insert(tx, "commit-insert", 5); err != nil {
		panic(err)
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}
	fmt.Println("after commit of insert, rows:", count()) // expect 5

	//
	// client.Tx() closure helper: commits when fn returns nil, rolls back when
	// fn returns an error, and rolls back then re-panics when fn panics.
	//

	// 3) client.Tx: DELETE then ROLLBACK by returning an error.
	//    The deleted rows are restored, and the sentinel error is returned
	//    as-is so errors.Is/As keep working.
	errAbort := errors.New("abort transaction")
	err = client.Tx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM EXAMPLE_TX WHERE NAME = ?`, "commit-insert"); err != nil {
			return err
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		panic(err)
	}
	fmt.Println("after client.Tx rollback of delete, rows:", count()) // expect 5

	// 4) client.Tx: DELETE then COMMIT by returning nil.
	err = client.Tx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM EXAMPLE_TX WHERE NAME = ?`, "commit-insert")
		return err
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("after client.Tx commit of delete, rows:", count()) // expect 0

	// 5) client.Tx: a panic inside fn rolls back and is re-raised.
	func() {
		defer func() {
			if p := recover(); p != "intentional panic" {
				panic(p)
			}
		}()
		_ = client.Tx(ctx, db, func(tx *sql.Tx) error {
			if err := insert(tx, "panic-insert", 5); err != nil {
				return err
			}
			panic("intentional panic")
		})
	}()
	fmt.Println("after client.Tx panic, rows:", count()) // expect 0

	// 6) client.TxConn: transaction on a specific connection.
	//    Use when the caller already holds a *sql.Conn (e.g. acquired via
	//    db.Conn(ctx) for connection-scoped work).
	conn, err := db.Conn(ctx)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	err = client.TxConn(ctx, conn, func(tx *sql.Tx) error {
		return insert(tx, "txconn-insert", 3)
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("after client.TxConn commit of insert, rows:", count()) // expect 3
}
