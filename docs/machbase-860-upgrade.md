# Machbase 8.6.0 Go 클라이언트 사용 안내서

이 문서는 Go 애플리케이션에서 Machbase 8.6.0의 Transaction table, DECIMAL, named parameter, NULL 처리와 transaction 기능을 사용하는 방법을 설명한다.

> **Edition 주의:** Transaction table은 Machbase 8.6.0 standard(single-node) edition에서 사용할 수 있다. cluster edition에서는 Transaction table을 생성할 수 없다.

## 1. 시작하기 전에

Go 1.22 이상과 Machbase 8.6.0을 지원하는 neo-client 배포본을 사용한다. 사용하는 client 버전을 확인하려면 애플리케이션 module 디렉터리에서 다음 명령을 실행한다.

```bash
go list -m github.com/machbase/neo-client
```

이 문서의 예제는 다음 접속 정보를 사용한다.

```text
server   127.0.0.1
port     5656
user     sys
password manager
```

예제는 고정 이름의 table을 생성하고 종료할 때 삭제한다. 격리된 개발용 database에서만 실행하고 운영 또는 공유 server에서는 실행하지 않는다. 같은 이름의 table이 이미 있으면 기존 table을 삭제하지 않고 오류로 종료한다.

실행 가능한 전체 예제는 다음 위치에 있다.

- [native API 예제](examples/machbase860-native/main.go)
- [`database/sql` 예제](examples/machbase860-database-sql/main.go)

```bash
go run ./docs/examples/machbase860-native
go run ./docs/examples/machbase860-database-sql
```

## 2. API 선택

| 요구 사항 | 권장 API |
|---|---|
| Go 표준 connection pool과 transaction | `database/sql` |
| Machbase table type과 appender | native `machgo`/`api` |
| DECIMAL의 precision과 scale 직접 확인 | native `api.Decimal` |
| 일반적인 서비스 애플리케이션 | `database/sql` |

두 API는 같은 database에 접속하지만 일부 값의 입력 및 조회 방법은 다르다. 특히 DECIMAL과 NULL 처리 시 각 절의 예제를 따른다.

## 3. 연결

### 3.1 native API

```go
ctx := context.Background()

database, err := machgo.NewDatabase(&machgo.Config{
    Host: "127.0.0.1",
    Port: 5656,
})
if err != nil {
    return err
}
defer database.Close()

conn, err := database.Connect(ctx, api.WithPassword("sys", "manager"))
if err != nil {
    return err
}
defer conn.Close()
```

### 3.2 database/sql

root package를 blank import하면 `machbase` driver가 등록된다.

```go
import (
    "database/sql"

    _ "github.com/machbase/neo-client"
)

db, err := sql.Open("machbase",
    "server=tcp://sys:manager@127.0.0.1:5656;statement_cache=auto")
if err != nil {
    return err
}
defer db.Close()

if err := db.PingContext(ctx); err != nil {
    return err
}
```

`sql.Open()`은 실제 연결을 즉시 만들지 않을 수 있으므로 애플리케이션 시작 시 `PingContext()`로 접속과 인증을 확인한다.

## 4. Transaction table

```go
result := conn.Exec(ctx, `CREATE TRANSACTION TABLE ACCOUNT (
    ID      INTEGER PRIMARY KEY,
    AMOUNT  DECIMAL(30,12),
    NOTE    VARCHAR(80)
)`)
if err := result.Err(); err != nil {
    return err
}
```

native API에서 Transaction table의 table type은 `api.TableTypeTransaction`이며 값은 `8`이다.

Transaction table은 primary key 없이도 만들 수 있다. UPDATE와 DELETE는 primary key equality뿐 아니라 일반 조건과 범위 조건을 사용할 수 있다.

```sql
UPDATE ACCOUNT SET NOTE = 'review' WHERE AMOUNT < 0;
DELETE FROM ACCOUNT WHERE ID >= 100 AND ID < 200;
```

primary key는 데이터 무결성과 조회 성능을 위해 필요에 따라 정의한다.

## 5. DECIMAL

### 5.1 정확한 값 만들기

`api.ParseDecimal(text, precision, scale)`로 문자열을 `api.Decimal` 값으로 변환한다.

```go
amount, err := api.ParseDecimal(
    "123456789012345678.901234567890",
    30,
    12,
)
if err != nil {
    return err
}
```

`api.Decimal`은 정수를 기반으로 소수 값을 보관하므로 `float64` 변환에서 발생할 수 있는 정밀도 손실을 피한다.

```text
실제 값 = 정수 값 × 10^-scale
```

금액이나 정밀 계측값은 `float64`로 먼저 변환하지 말고 원본 문자열을 바로 `ParseDecimal()`에 전달한다.

### 5.2 범위와 반올림

- precision: 1~65
- scale: 0~30, precision 이하
- scale보다 긴 소수부: 0.5를 0에서 멀어지는 방향으로 반올림
- 반올림 후 precision 초과: 오류

```go
positive, _ := api.ParseDecimal("1.235", 10, 2)  // 1.24
negative, _ := api.ParseDecimal("-1.235", 10, 2) // -1.24

_, err := api.ParseDecimal("999.5", 3, 0) // 1000이 필요하므로 오류
```

### 5.3 입력

positional parameter로 입력한다.

```go
result := conn.Exec(ctx,
    "INSERT INTO ACCOUNT VALUES (?, ?, ?)",
    int32(1), amount, "exact")
if err := result.Err(); err != nil {
    return err
}
```

`database/sql`에서도 `api.Decimal`을 parameter 값으로 전달할 수 있다.

```go
_, err = db.ExecContext(ctx,
    "INSERT INTO ACCOUNT VALUES (?, ?, ?)",
    int32(1), amount, "exact")
```

### 5.4 native API 조회

```go
var got api.Decimal
err := conn.QueryRow(ctx,
    "SELECT AMOUNT FROM ACCOUNT WHERE ID=?", int32(1)).Scan(&got)
if err != nil {
    return err
}

fmt.Println(got.String())
fmt.Printf("precision=%d scale=%d\n", got.Precision(), got.Scale())
```

`DECIMAL(30,12)` 예제의 출력은 다음과 같다.

```text
123456789012345678.901234567890
precision=30 scale=12
```

### 5.5 database/sql 조회

`database/sql`에서는 DECIMAL 값을 정확한 문자열로 받는다.

```go
var text string
if err := db.QueryRowContext(ctx,
    "SELECT AMOUNT FROM ACCOUNT WHERE ID=?", int32(1)).Scan(&text); err != nil {
    return err
}

amount, err := api.ParseDecimal(text, 30, 12)
if err != nil {
    return err
}
```

조회 대상의 precision과 scale을 미리 알 수 없는 경우 `ColumnType.DecimalSize()`를 사용한다.

```go
rows, err := db.QueryContext(ctx, "SELECT AMOUNT FROM ACCOUNT")
if err != nil {
    return err
}
defer rows.Close()

columnTypes, err := rows.ColumnTypes()
if err != nil {
    return err
}
precision, scale, ok := columnTypes[0].DecimalSize()
if !ok {
    return errors.New("DECIMAL precision과 scale을 확인할 수 없음")
}
```

NULL 가능 DECIMAL은 `sql.NullString`으로 받은 뒤 `Valid`를 확인하고 선언된 precision과 scale로 변환하는 방법을 권장한다.

```go
for rows.Next() {
    var text sql.NullString
    if err := rows.Scan(&text); err != nil {
        return err
    }
    if !text.Valid {
        fmt.Println("AMOUNT is NULL")
        continue
    }

    value, err := api.ParseDecimal(
        text.String, int(precision), int(scale))
    if err != nil {
        return err
    }
    fmt.Println(value.String())
}
if err := rows.Err(); err != nil {
    return err
}
```

## 6. named parameter

Machbase 8.6.0에서는 SQL parameter에 이름을 지정할 수 있다.

### 6.1 native API

```go
result := conn.Exec(ctx,
    "INSERT INTO ACCOUNT VALUES (:id, :amount, :note)",
    api.Named("note", "native"),
    api.Named("amount", amount),
    api.Named("id", int32(1)))
```

### 6.2 database/sql

```go
_, err := db.ExecContext(ctx,
    "INSERT INTO ACCOUNT VALUES (:id, :amount, :note)",
    sql.Named("note", "database/sql"),
    sql.Named("amount", amount),
    sql.Named("id", int32(2)))
```

named parameter에는 다음 규칙이 적용된다.

- 이름은 대소문자를 구분한다.
- argument 순서는 SQL에 나타난 parameter 순서와 달라도 된다.
- 같은 이름이 여러 번 나타나면 값을 한 번만 전달한다.
- 누락된 이름, 사용되지 않는 이름, 중복 전달은 오류다.
- named parameter와 positional parameter를 한 실행에서 혼합하지 않는다.
- 한 SQL 문장에는 최대 256개의 parameter를 사용할 수 있다.

```go
row := conn.QueryRow(ctx,
    "SELECT ID FROM ACCOUNT WHERE ID=:id OR PARENT_ID=:id",
    api.Named("id", int32(1)))
```

native API의 NULL parameter에는 `nil`을 사용한다. `database/sql`에서는 `nil` 또는 유효한 `sql.Null[T]` 값을 사용할 수 있다. `(*int32)(nil)`과 같은 typed nil pointer는 사용하지 않는다.

`sql.Named()`의 이름은 문자로 시작해야 한다. 두 API에서 함께 사용할 이름은 `id`, `amount2`처럼 문자로 시작하도록 작성한다.

## 7. NULL 처리

### 7.1 native API

NULL 가능 column은 `sql.NullString`, `sql.NullInt32`, `sql.Null[api.Decimal]` 같은 nullable destination으로 받는다.

```go
var amount sql.Null[api.Decimal]
var note sql.NullString

err := conn.QueryRow(ctx,
    "SELECT AMOUNT, NOTE FROM ACCOUNT WHERE ID=?", int32(2)).
    Scan(&amount, &note)
if err != nil {
    return err
}

if !amount.Valid {
    fmt.Println("AMOUNT is NULL")
}
```

NULL을 일반 문자열, 숫자 또는 `api.Decimal` destination으로 받으면 오류가 발생한다. nullable destination을 여러 행에서 재사용할 때는 항상 `Valid`를 먼저 확인한다.

### 7.2 database/sql

```go
var amount sql.NullString
var note sql.NullString

err := db.QueryRowContext(ctx,
    "SELECT AMOUNT, NOTE FROM ACCOUNT WHERE ID=?", int32(2)).
    Scan(&amount, &note)
if err != nil {
    return err
}
```

column의 NULL 허용 여부는 `ColumnType.Nullable()`로 확인할 수 있다. 반환되는 `ok`가 false이면 NULL 허용 여부를 판단할 수 없다는 의미다.

## 8. database/sql transaction

application transaction에는 `sql.Tx`를 사용한다.

```go
func updateAccount(ctx context.Context, db *sql.DB, amount api.Decimal) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    result, err := tx.ExecContext(ctx,
        "UPDATE ACCOUNT SET AMOUNT=? WHERE ID>=? AND ID<=?",
        amount, int32(10), int32(12))
    if err != nil {
        return err
    }

    matched, err := result.RowsAffected()
    if err != nil {
        return err
    }
    fmt.Printf("update matched=%d\n", matched)

    return tx.Commit()
}
```

지원 범위와 주의사항은 다음과 같다.

- 기본 isolation level을 사용한다.
- read-only transaction과 별도 isolation level은 지원하지 않는다.
- `RowsAffected()`는 INSERT 성공 행 수 또는 UPDATE/DELETE 조건에 일치한 행 수를 반환한다.
- `LastInsertId()`는 지원하지 않는다.
- 완료된 transaction을 다시 사용하면 `sql.ErrTxDone`이 반환된다.
- transaction에서 연 `Rows`는 Commit 또는 Rollback 전에 닫는다.
- transaction context가 취소되면 rollback되므로 같은 transaction에서 Commit을 재시도하지 않는다.

직접 `BEGIN`, `COMMIT`, `ROLLBACK`을 실행해야 한다면 하나의 `*sql.Conn`을 사용한다. 각각을 `sql.DB.ExecContext()`로 실행하면 서로 다른 connection이 선택될 수 있다.

```go
conn, err := db.Conn(ctx)
if err != nil {
    return err
}
defer conn.Close()

if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
    return err
}
// 같은 conn으로 DML과 COMMIT 또는 ROLLBACK을 실행한다.
```

## 9. Appender

native API의 Appender로 Transaction table에 여러 행을 입력할 수 있다.

```go
appender, err := conn.Appender(ctx, "ACCOUNT")
if err != nil {
    return err
}
appender = appender.WithBatchMaxRows(1000)

appendErr := appender.Append(int32(3), amount, "appended")
success, failed, closeErr := appender.Close()
if err := errors.Join(appendErr, closeErr); err != nil {
    return fmt.Errorf("append/close: success=%d failed=%d: %w",
        success, failed, err)
}
if success != 1 || failed != 0 {
    return fmt.Errorf("unexpected append counts: success=%d failed=%d",
        success, failed)
}
```

Appender 사용 시 다음 사항에 주의한다.

- Appender는 `sql.Tx`에 포함되지 않는다.
- 입력한 batch는 각각 독립적으로 반영된다.
- 이미 반영된 batch는 이후 `sql.Tx.Rollback()`으로 취소할 수 없다.
- `Append()`가 실패해도 `Close()`를 호출한다.
- `Close()`의 success, failed, error를 모두 확인한다.
- batch 크기는 `WithBatchMaxRows()`, `WithBatchMaxBytes()`, `WithBatchMaxDelay()`로 설정한다.

## 10. prepared statement 재사용

Machbase 8.6.0에서는 table을 다시 생성하여 result column type이 변경된 경우에도 기존 prepared statement를 재사용할 수 있다.

```go
stmt, err := db.PrepareContext(ctx,
    "SELECT VALUE FROM CONFIG_TABLE WHERE ID=:id")
if err != nil {
    return err
}
defer stmt.Close()

var value string
if err := stmt.QueryRowContext(ctx,
    sql.Named("id", int32(1))).Scan(&value); err != nil {
    return err
}
```

애플리케이션은 `Stmt.Close()`를 호출하여 사용이 끝난 statement 자원을 해제해야 한다.

## 11. Primary key 메타데이터

Native API의 `Rows.Columns()`와 `Row.Columns()`는 각 결과 컬럼의 `PrimaryKey` 값을 제공한다.

```go
rows, err := conn.Query(ctx, `SELECT ID, AMOUNT, ID + 1 AS NEXT_ID FROM ORDERS`)
if err != nil {
	log.Fatal(err)
}
defer rows.Close()

columns, err := rows.Columns()
if err != nil {
	log.Fatal(err)
}
for _, column := range columns {
	fmt.Printf("name=%s primary=%v\n", column.Name, column.PrimaryKey)
}
```

`ID`가 `ORDERS`의 primary key라면 `ID`는 `true`, 일반 컬럼인 `AMOUNT`와 계산식인 `NEXT_ID`는 `false`이다. 집계 결과와 outer join의 nullable 측 컬럼도 `false`이다. Tag table에서는 직접 조회한 `NAME` 컬럼이 primary key로 표시된다.

Go 표준 `database/sql.ColumnType`에는 primary key 여부를 반환하는 메서드가 없다. 이 정보가 필요한 애플리케이션은 `machgo` Native API의 `api.Rows.Columns()` 또는 `api.Row.Columns()`를 사용해야 한다.

## 12. Machbase 8.5.x 호환

Machbase 8.5.x server에 연결할 때는 기존 positional parameter를 사용한다.

```go
var name string
err := db.QueryRowContext(ctx,
    "SELECT NAME FROM T WHERE ID=?", int32(1)).Scan(&name)
```

| 기능 | Machbase 8.6.0 | Machbase 8.5.x |
|---|---|---|
| 기존 table과 data type의 positional query | 지원 | 지원 |
| Transaction table | standard edition에서 지원 | 지원하지 않음 |
| DECIMAL | 지원 | 지원하지 않음 |
| named parameter | 지원 | 지원하지 않음 |
| NULL 허용 여부 조회 | 지원 | 일부 column에서 판정 불가 |
| SQL parameter 수 | 최대 256개 | 최대 255개 |

Machbase 8.5.x에서는 `:id` 형식 대신 `?`를 사용하고 값을 SQL에 나타난 순서대로 전달한다.

## 13. 자주 발생하는 오류와 해결 방법

이 절은 현재 제품 버그 목록이 아니다. 설치된 client 버전, API 사용 방법 또는 server 버전 차이로 발생할 수 있는 일반적인 문제를 설명한다.

### 신규 API가 정의되지 않았다고 나옴

`api.Decimal`, `api.Named` 또는 `TableTypeTransaction`을 찾을 수 없다면 Machbase 8.6.0을 지원하는 neo-client 배포본을 사용하고 있는지 확인한다.

```bash
go list -m github.com/machbase/neo-client
```

### NULL과 0 또는 빈 문자열을 구분할 수 없음

일반 destination 대신 `sql.NullString`, `sql.NullInt32`, `sql.Null[T]`를 사용하고 `Valid`를 먼저 확인한다.

### DECIMAL의 precision과 scale이 예상과 다름

`database/sql`에서는 DECIMAL을 `sql.NullString`으로 받은 뒤 `ColumnType.DecimalSize()`가 반환한 precision과 scale로 `api.ParseDecimal()`을 호출한다.

### transaction 종료 시 resource busy 오류가 발생함

transaction에서 연 모든 `Rows`를 먼저 닫고 iteration 후 `Rows.Err()`를 확인한 다음 Commit 또는 Rollback한다.

### Machbase 8.5.x에서 named parameter가 실패함

Machbase 8.5.x는 named parameter를 지원하지 않는다. SQL의 parameter를 `?`로 바꾸고 positional argument를 사용한다.
