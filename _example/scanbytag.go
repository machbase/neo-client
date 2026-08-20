//go:build example
// +build example

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	client "github.com/machbase/neo-client/v2"
)

var server = "127.0.0.1:5656"
var user = "sys"
var password = "manager"

// TagRecord maps columns by the `db` struct tag. Untagged fields are excluded,
// and a NULL column can be received as a nil pointer.
type TagRecord struct {
	Name  string    `db:"NAME"`
	Time  time.Time `db:"TIME"`
	Value float64   `db:"VALUE"`

	cachedLabel string // unexported fields are always ignored
}

func main() {
	flag.StringVar(&server, "s", server, "server address")
	flag.StringVar(&user, "u", user, "user")
	flag.StringVar(&password, "p", password, "password")
	flag.Parse()

	fields := []string{}
	fields = append(fields, "server="+server)
	fields = append(fields, "user="+user)
	fields = append(fields, "password="+password)

	db, err := sql.Open("machbase", strings.Join(fields, ";"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `CREATE TAG TABLE IF NOT EXISTS EXAMPLE (
		NAME   VARCHAR(100)  PRIMARY KEY,
		TIME   DATETIME      BASE TIME,
		VALUE  DOUBLE
	)`)
	if err != nil {
		panic(err)
	}

	ts := time.Now()
	for i := 0; i < 10; i++ {
		_, err := db.ExecContext(ctx, `INSERT INTO EXAMPLE VALUES (?, ?, ?)`,
			"example-scan", ts.Add(time.Second*time.Duration(i)), 3.14*float64(i))
		if err != nil {
			panic(err)
		}
	}
	_, err = db.ExecContext(ctx, `EXEC TABLE_FLUSH(EXAMPLE)`)
	if err != nil {
		panic(err)
	}

	selectAll := `SELECT NAME, TIME, VALUE FROM EXAMPLE WHERE NAME = ? ORDER BY TIME`

	// 1. Select scans every row into a slice of structs.
	//    It is bounded by WithMaxRows (1000 by default).
	records, err := client.Select[TagRecord](ctx, db, selectAll, "example-scan")
	if err != nil {
		panic(err)
	}
	fmt.Println("Select:", len(records), "records")
	for _, rec := range records[:3] {
		fmt.Printf("  %s %s %v\n", rec.Name, rec.Time.Format(time.RFC3339), rec.Value)
	}

	// 2. Get scans a single row and returns sql.ErrNoRows when there is none.
	first, err := client.Get[TagRecord](ctx, db,
		`SELECT NAME, TIME, VALUE FROM EXAMPLE WHERE NAME = ? LIMIT 1`, "example-scan")
	if err != nil {
		panic(err)
	}
	fmt.Println("Get:", first.Name, first.Value)

	// 3. ScanAll works on any *sql.Rows the caller already has.
	//    The helpers never close rows; closing stays the caller's responsibility.
	rows, err := db.QueryContext(ctx, selectAll, "example-scan")
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	scanned, err := client.ScanAll[TagRecord](rows)
	if err != nil {
		panic(err)
	}
	fmt.Println("ScanAll:", len(scanned), "records")

	// 4. ScanEach streams the result set with only one record alive at a time,
	//    which is the safe choice for unbounded queries.
	streamRows, err := db.QueryContext(ctx, selectAll, "example-scan")
	if err != nil {
		panic(err)
	}
	defer streamRows.Close()
	var total float64
	err = client.ScanEach(streamRows, func(rec TagRecord) error {
		total += rec.Value
		return nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("ScanEach: total %.2f\n", total)

	// 5. NamedArgs builds sql.Named arguments from a struct or a map using the
	//    same `db` tags. The SQL text is passed to the server unchanged.
	named, err := client.SupportsNamedParameters(ctx, db)
	if err != nil {
		panic(err)
	}
	if !named {
		fmt.Println("NamedArgs: server does not support named parameters")
		return
	}
	args, err := client.NamedArgs(struct {
		Name string `db:"name"`
	}{Name: "example-scan"})
	if err != nil {
		panic(err)
	}
	count, err := client.Get[int64](ctx, db,
		`SELECT COUNT(*) FROM EXAMPLE WHERE NAME = :name`, args...)
	if err != nil {
		panic(err)
	}
	fmt.Println("NamedArgs: count", count)
}
