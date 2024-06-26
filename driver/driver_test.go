package driver_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/machbase/neo-client/driver"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	svr := &MockServer{}

	err := svr.Start()
	if err != nil {
		panic(err)
	}

	m.Run()

	svr.Stop()
}

func connect(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("machbase", fmt.Sprintf("tcp://sys:manager@%s", MockServerAddr))
	if err != nil {
		t.Fatalf("db connection failure %q", err.Error())
	}
	return db
}

func TestConnectFail(t *testing.T) {
	tests := []string{
		fmt.Sprintf("tcp://%s", MockServerAddr),
		fmt.Sprintf("tcp://sys:man@%s", MockServerAddr),
		fmt.Sprintf("tcp://sys@%s", MockServerAddr),
	}
	for _, tt := range tests {
		db, err := sql.Open("machbase", tt)
		require.NotNil(t, err)
		require.Equal(t, "invalid username or password", err.Error())
		require.Nil(t, db)
	}
}

func TestQuery(t *testing.T) {
	db := connect(t)
	defer db.Close()

	rows, err := db.Query(`select * from example where name = ?`, "query1")
	require.Nil(t, err)
	require.NotNil(t, rows)
	rows.Close()

	conn, err := db.Conn(context.TODO())
	require.Nil(t, err)
	require.NotNil(t, conn)

	rows, err = conn.QueryContext(context.TODO(), `select * from example where name = ?`, "query1")
	require.Nil(t, err)
	require.NotNil(t, rows)
	rows.Close()

	conn.Close()
}

func TestDriver(t *testing.T) {
	driver.RegisterDataSource("local-unix", &driver.DataSource{
		ServerAddr: "unix://../../neo-server/tmp/mach-grpc.sock",
		User:       "sys",
		Password:   "manager",
	})

	driver.RegisterDataSource("local-tcp", &driver.DataSource{
		ServerAddr: "tcp://127.0.0.1:5655",
		User:       "sys",
		Password:   "manager",
	})

	testDriverDataSource(t, "local-unix")
	testDriverDataSource(t, "local-tcp")
}

func testDriverDataSource(t *testing.T, dataSourceName string) {
	db, err := sql.Open(driver.Name, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	require.NotNil(t, db)

	var tableName = strings.ToUpper("tagdata")
	var count int

	row := db.QueryRow("select count(*) from M$SYS_TABLES where name = ?", tableName)
	if row.Err() != nil {
		t.Fatal(row.Err())
	}
	err = row.Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		sqlText := fmt.Sprintf(`
			create tag table %s (
				name            varchar(200) primary key,
				time            datetime basetime,
				value           double summarized,
				type            varchar(40),
				ivalue          long,
				svalue          varchar(400),
				id              varchar(80),
				pname           varchar(80),
				sampling_period long,
				payload         json
			)`, tableName)
		_, err := db.Exec(sqlText)
		if err != nil {
			t.Error(err)
		}

		row := db.QueryRow("select count(*) from M$SYS_TABLES where name = ?", tableName)
		if row.Err() != nil {
			t.Error(row.Err())
		}
		err = row.Scan(&count)
		if err != nil {
			t.Error(err)
		}
	}
	require.Equal(t, 1, count)

	expectCount := 10000
	ts := time.Now()
	for i := 0; i < expectCount; i++ {
		result, err := db.Exec("insert into "+tableName+" (name, time, value, id) values(?, ?, ?, ?)",
			fmt.Sprintf("name-%d", count%5),
			ts.Add(time.Duration(i)),
			0.1001+0.1001*float32(count),
			fmt.Sprintf("id-%08d", i))
		if err != nil {
			t.Error(err)
		}
		require.Nil(t, err)
		nrows, _ := result.RowsAffected()
		require.Equal(t, int64(1), nrows)
	}

	rows, err := db.Query("select name, time, value, id from "+tableName+" where time >= ? order by time", ts)
	if err != nil {
		t.Error(err)
	}
	pass := 0
	for rows.Next() {
		var name string
		var ts time.Time
		var value float64
		var id string
		err := rows.Scan(&name, &ts, &value, &id)
		if err != nil {
			t.Logf("ERR> %v", err.Error())
			break
		}
		require.Equal(t, fmt.Sprintf("name-%d", count%5), name)
		pass++
		//t.Logf("==> %v %v %v %v", name, ts, value, id)
	}
	rows.Close()
	require.Equal(t, expectCount, pass)

	r := db.QueryRow("select count(*) from "+tableName+" where time >= ?", ts)
	r.Scan(&count)
	require.Equal(t, expectCount, count)
	t.Logf("DB=%#v", db.Stats())
}
