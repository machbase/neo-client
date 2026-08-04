# Machbase 8.6 Go 클라이언트 실전 안내서

이 문서는 Machbase main trunk의 CMI 4.0.3 기능을 사용하는 Go 개발자를 위한 입문서이자 호환성 안내서다. 단순 API 목록이 아니라 어떤 값을 넣고, 무엇을 확인하며, 어느 동작까지 회귀 검증됐는지를 함께 설명한다.

> **Edition 주의:** 이 문서의 `CREATE TRANSACTION TABLE` 실습은 현재 **standard(single-node) edition 전용**이다. cluster edition은 Transaction table 생성을 거부한다. cluster에서 그대로 실행할 수 있는 예제로 오해하면 안 된다.

## 1. 구현 소스 선택

현재 기능은 `neo-client`의 `2-machbase-860-upgrade` 브랜치에서 개발됐다. 이 기능을 포함하는 정식 release가 나오기 전에는 `@latest`를 사용하면 안 된다. 현재 latest tag가 신규 API를 포함한다고 보장할 수 없기 때문이다.

로컬 checkout으로 개발할 때는 `go.work`가 가장 명확하다.

```bash
git clone https://github.com/machbase/neo-client.git
cd neo-client
git checkout 2-machbase-860-upgrade

cd /path/to/my-application
go work init . /path/to/neo-client
go list -m github.com/machbase/neo-client
```

CI처럼 checkout을 공유할 수 없다면 검증한 정확한 commit 또는 merge 후 발행된 release를 고정한다. 현재 기능 구현이 완료된 검증 commit을 선택하는 예는 다음과 같다.

```bash
go get github.com/machbase/neo-client@203c6dc
go list -m github.com/machbase/neo-client
```

`@2-machbase-860-upgrade` 같은 moving branch 지정은 최신 branch를 잠깐 시험할 때만 사용하고 CI 입력으로 고정하지 않는다. `go.mod`에 기록된 실제 pseudo-version과 `go.sum`을 함께 commit한다.

nfx 회귀 테스트는 로컬 branch를 다음처럼 연결한다.

```bash
cd ${MACHBASEDEV_HOME}/test/regress/golang
NEO_CLIENT_SOURCE=${HOME}/work/neo-client itf golang.ts
```

merge 후에는 nfx `go.mod`를 merge commit의 정확한 pseudo-version 또는 release로 갱신하고 `NEO_CLIENT_SOURCE` 없이 실행하는 것이 최종 검증 경로다.

## 2. 어떤 Go API를 선택할까

| 요구 사항 | 권장 API | 이유 |
|---|---|---|
| Go 표준 pool과 transaction | `database/sql` | `sql.DB`, `sql.Tx`, `sql.Stmt` 계약 사용 |
| Machbase table type과 appender | native `machgo`/`api` | Machbase 고유 metadata와 appender 공개 |
| DECIMAL의 precision/scale 직접 보존 | native `api.Decimal` scan | wire column shape를 그대로 복원 |
| 일반 서비스 코드 | 우선 `database/sql` | 표준 lifecycle과 context 관리 |

두 API는 같은 protocol 구현을 사용하지만 scan destination과 transaction surface가 완전히 같지는 않다. 이 차이를 뒤에서 명시한다.

## 3. 실행 가능한 기본 학습 예제

- [native API 예제](examples/machbase860-native/main.go)
- [`database/sql` 예제](examples/machbase860-database-sql/main.go)

예제는 기본적으로 `127.0.0.1:5656`, `sys/manager`에 접속하며 standard edition에서 실행한다. 고정 이름의 실습 table을 생성하고 종료 시 삭제하므로 **격리된 개발 DB에서만 실행하고 운영·공유 server에서는 실행하지 않는다.** 이미 같은 이름의 table이 있으면 안전하게 CREATE 오류로 종료하며 기존 table을 먼저 DROP하지 않는다.

```bash
go run ./docs/examples/machbase860-native
go run ./docs/examples/machbase860-database-sql
```

native 예제의 핵심 출력은 다음과 같다.

```text
insert affected=1
row id=1 amount=123456789012345678.901234567890 precision=30 scale=12 note=named insert
nullable amount.valid=false note.valid=false
table_type=TransactionTable(8)
append success=1 failed=0
update matched=2
```

`database/sql` 예제는 rollback과 commit을 실제 조회로 구분한다.

```text
insert affected=1
last_insert_id unsupported=true
amount=123.450000000000 precision=30 scale=12
nullable amount.valid=false note.valid=false
after rollback count=0
after commit count=1
prepared id=1 note="database/sql"
prepared id=11 note="commit"
```

그 뒤의 `list` 행은 NULL의 `Valid` 상태와 commit된 최종 row들을 보여준다. 출력 문자열만 맞추는 것이 아니라 affected count, exact Decimal, NULL 구분, rollback visibility를 각각 관측하는 것이 핵심이다.

두 예제는 `main()` 안에서 `log.Fatal()`을 호출하지 않는다. `log.Fatal()`은 `os.Exit()`을 실행해 table 삭제, statement close 같은 defer를 건너뛰기 때문이다. `run() error`가 자원을 정리한 뒤 `main()`이 종료 코드를 결정한다.

## 4. 연결

### 4.1 native API

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

### 4.2 database/sql

root package를 blank import해야 driver가 `machbase`라는 이름으로 등록된다.

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

`sql.Open()`은 실제 연결을 즉시 보장하지 않으므로 시작 단계에서 `PingContext()`로 인증과 network 상태를 확인한다.

## 5. Transaction table

Transaction table은 catalog와 native appender에서 `api.TableTypeTransaction`, 숫자값 `8`로 노출된다.

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

### 5.1 PK는 필수가 아니다

Transaction table은 primary key 없이도 만들 수 있고, UPDATE/DELETE도 일반 predicate와 다건 범위를 지원한다. 즉 “WHERE에는 반드시 PK가 있어야 한다”는 규칙은 Transaction table에 적용되지 않는다.

```sql
UPDATE ACCOUNT SET NOTE = 'review' WHERE AMOUNT < 0;
DELETE FROM ACCOUNT WHERE ID >= 100 AND ID < 200;
```

PK는 중복 방지, 데이터 무결성, 검색 성능을 위해 권장할 수 있지만 문법·실행의 필수 조건은 아니다. `RowsAffected()`는 0건, 1건, 다건 모두 반환한다.

### 5.2 활성 transaction의 범위

명시적 transaction 안에서는 Transaction table의 DML, SELECT, TRUNCATE를 사용할 수 있다. transaction DDL과 non-Transaction table에 대한 write/DDL은 제한된다. non-Transaction table SELECT와 join은 허용되는 경로가 있다. 여러 Transaction table을 한 transaction에서 다룰 수 있어도 장애 시점까지 포함한 분산/global atomicity를 별도 보장한다고 확대 해석해서는 안 된다.

## 6. DECIMAL을 정확하게 입력하기

현재 회귀 suite는 SQL `DECIMAL`을 독립적으로 검증한다. 서버가 `NUMERIC`을 별칭으로 제공하더라도 이 문서에서는 별도의 `NUMERIC` DDL 검증을 수행했다고 주장하지 않는다.

`api.ParseDecimal(text, precision, scale)`은 문자열을 exact fixed-point 값으로 만든다.

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

`amount`의 Go 타입은 `api.Decimal`이다. 내부 개념은 다음과 같다.

```text
실제 값 = unscaled(big.Int) × 10^-scale
```

`123.45`를 `(precision=10, scale=2)`로 만들면 `unscaled=12345`다. binary floating-point를 거치지 않으므로 큰 정수부와 긴 소수부를 정확하게 유지한다.

### 6.1 반올림과 overflow

precision은 1~65, scale은 0~30이며 scale은 precision보다 클 수 없다. 입력의 소수 자릿수가 target scale보다 길면 half-away-from-zero 방식으로 반올림한다.

```go
positive, _ := api.ParseDecimal("1.235", 10, 2)  // 1.24
negative, _ := api.ParseDecimal("-1.235", 10, 2) // -1.24
```

반올림 결과가 precision을 넘는 경우도 오류다.

```go
_, err := api.ParseDecimal("999.5", 3, 0) // 1000이 필요하므로 overflow
if err == nil {
    return errors.New("overflow가 거부되지 않음")
}
```

오류 문자열 전체는 공개 API 계약으로 간주하지 않는다. 보통 `err != nil`을 확인하고 필요한 경우 감싼 오류의 type/sentinel만 검사한다.

### 6.2 입력 방법

```go
result := conn.Exec(ctx,
    "INSERT INTO ACCOUNT VALUES (?, ?, ?)",
    int32(1), amount, "exact")
if err := result.Err(); err != nil {
    return err
}
```

금액을 먼저 `float64`로 만들었다가 문자열로 바꾸면 `ParseDecimal()` 전에 이미 정보가 손실될 수 있다. 원본 JSON/CSV/사용자 입력 문자열을 바로 parse하는 방법이 안전하다.

query bind의 일반 compact carrier에 직접 들어가지 않는 36~65자리 정수부도 유효한 `api.Decimal`이면 exact text carrier로 전환된다. 이 경로는 INSERT처럼 **대상 DECIMAL type이 명확한 bind context**에서 검증됐다. `SELECT ?`, overload 함수, untyped expression에서는 fallback payload가 VARCHAR로 보일 수 있어 type 선택이나 result metadata가 달라질 수 있으므로 같은 보장을 확대 적용하지 않는다. 명확한 대상 column의 최종 precision/scale 변환과 overflow 검사는 서버가 수행한다.

## 7. DECIMAL 조회와 출력

### 7.1 native: column의 precision/scale 보존

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

`DECIMAL(30,12)`에 저장한 예제의 출력은 다음과 같다.

```text
123456789012345678.901234567890
precision=30 scale=12
```

### 7.2 database/sql: exact text로 받은 뒤 선언 shape로 parse

driver는 DECIMAL result를 정밀도 손실 없는 문자열로 `database/sql`에 전달한다.

```go
var text string
if err := db.QueryRowContext(ctx,
    "SELECT AMOUNT FROM ACCOUNT WHERE ID=?", int32(1)).Scan(&text); err != nil {
    return err
}

amount, err := api.ParseDecimal(text, 30, 12)
```

여러 query를 일반화해야 한다면 `Rows.ColumnTypes()`와 `ColumnType.DecimalSize()`로 선언 precision/scale을 얻는다.

```go
rows, err := db.QueryContext(ctx, "SELECT AMOUNT FROM ACCOUNT")
if err != nil {
    return err
}
defer rows.Close()

types, err := rows.ColumnTypes()
if err != nil {
    return err
}
precision, scale, ok := types[0].DecimalSize()
if !ok {
    return errors.New("DECIMAL precision/scale metadata를 확인할 수 없음")
}
```

반복문은 마지막에 반드시 `Rows.Err()`를 확인한다.

```go
for rows.Next() {
    var text sql.NullString
    if err := rows.Scan(&text); err != nil {
        return err
    }
    if text.Valid {
        value, err := api.ParseDecimal(text.String, int(precision), int(scale))
        if err != nil {
            return err
        }
        fmt.Println(value.String())
    }
}
if err := rows.Err(); err != nil {
    return err
}
```

zero-value `sql.Null[api.Decimal]`에 직접 scan하면 `api.Decimal.Scan`의 기본 shape가 적용될 수 있어 선언된 `(30,12)`와 다른 `(65,30)` 형태가 될 수 있다. 특히 같은 destination을 NULL/non-NULL 행에 재사용하며 내부 값을 미리 설정하는 방식은 취약하다. `database/sql`에서는 `sql.NullString`으로 NULL을 구분한 뒤 선언 precision/scale로 parse하는 위 패턴을 권장한다.

## 8. named bind parameter

CMI 4.0.3 양단에서 native는 `api.Named()`, 표준 driver는 `sql.Named()`을 사용한다.

```go
result := conn.Exec(ctx,
    "INSERT INTO ACCOUNT VALUES (:id, :amount, :note)",
    api.Named("note", "native"),
    api.Named("amount", amount),
    api.Named("id", int32(1)))
```

```go
_, err := db.ExecContext(ctx,
    "INSERT INTO ACCOUNT VALUES (:id, :amount, :note)",
    sql.Named("note", "database/sql"),
    sql.Named("amount", amount),
    sql.Named("id", int32(2)))
```

핵심 규칙은 다음과 같다.

- 이름은 대소문자를 구분한다.
- argument 순서는 SQL marker 순서와 달라도 된다.
- 같은 marker가 반복되면 값을 한 번만 공급한다.
- supplied name의 누락, 초과, 중복은 오류다.
- named와 positional argument는 한 실행에서 섞지 않는다.
- 문자열 literal, quoted identifier, line/block comment의 `:name`은 marker가 아니다.

```go
row := conn.QueryRow(ctx,
    "SELECT ID FROM ACCOUNT WHERE ID=:id OR PARENT_ID=:id",
    api.Named("id", int32(1)))
```

native marker grammar는 `_value`, `$value` 같은 profile을 서버 grammar 범위에서 지원할 수 있다. 반면 `database/sql`의 `sql.Named` 이름은 Go 표준 검사를 먼저 통과해야 하며 첫 rune이 Unicode 문자여야 한다. 공통으로 사용할 코드는 `id`, `amount2`처럼 문자로 시작하는 이름을 선택한다.

CMI V2 ordinal은 256번째 parameter까지 표현하고, 257개 SQL parameter는 서버의 `QPC_MAX_BIND_PARAM_COUNT=256` 상한으로 거부된다. 이를 protocol `uint16` 자체의 최대치라고 설명하면 안 된다.

native `conn.Exec`/`api.Named`의 SQL NULL 입력에는 untyped `nil`을 사용한다. `sql.Null[T]` 입력은 `database/sql`이 `driver.Valuer`로 정규화하는 bind 경로에서 사용하며 native bind에 직접 전달하지 않는다. `sql.Null[T]`는 native에서는 scan destination으로 사용할 수 있다. `(*int32)(nil)`, `(*string)(nil)` 같은 typed nil pointer는 현재 별도 결함 탐색 대상이므로 입력 예제로 권장하지 않는다.

## 9. NULL과 nullable metadata

CMI 4.0.3 result metadata는 세 상태를 표현한다.

| 상태 | 의미 |
|---|---|
| `api.NullabilityUnknown` | 서버가 판정 정보를 보내지 않음 |
| `api.NullabilityNoNulls` | NULL 불가 |
| `api.NullabilityNullable` | NULL 허용 |

native API는 NULL을 plain `string`, 정수, `api.Decimal`에 scan하지 않는다. nullable wrapper를 사용한다.

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

native wrapper를 반복 재사용할 때 NULL 행은 `Valid=false`가 되고 이전 payload도 zero value로 지워진다. native `[]byte`와 `driver.Value` destination은 NULL에서 `nil`이 된다.

`database/sql`에서는 `sql.NullString`, `sql.NullInt32` 같은 표준 wrapper를 우선 사용한다. `database/sql`의 `*driver.Value` scan은 지원되는 NULL destination이 아니며 native API와 다르다.

Machbase 8.5.2는 nullable metadata를 보내지 않으므로 `ColumnType.Nullable()`은 `(false, false)`, 즉 값이 false라서 NOT NULL인 것이 아니라 `ok=false`라 판정 불가임을 뜻한다.

## 10. database/sql transaction

애플리케이션 transaction에는 raw SQL보다 `sql.Tx`를 권장한다.

```go
func transfer(ctx context.Context, db *sql.DB, amount api.Decimal) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback() // Commit 뒤에는 sql.ErrTxDone이며 무해하다.

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

지원 계약은 다음과 같다.

- 기본 isolation만 지원한다.
- read-only와 custom isolation은 오류다.
- `RowsAffected()`는 INSERT 성공 행 수, UPDATE/DELETE의 WHERE 일치 행 수를 반환한다.
- UPDATE가 같은 값을 다시 써도 WHERE에 일치하면 affected row로 계산한다.
- `LastInsertId()`는 지원하지 않는다.
- Commit/Rollback을 마친 `sql.Tx` 재사용은 `errors.Is(err, sql.ErrTxDone)`으로 확인한다.

열린 `Rows`가 남아 있으면 connection resource가 busy해 Commit/Rollback을 방해할 수 있다. transaction에서 조회한 rows를 먼저 `Close()`하고 `Rows.Err()`까지 확인한다.

`BeginTx`에 준 context가 취소되면 `database/sql`이 rollback한다. 그 뒤 `Commit()`은 timing에 따라 `context.Canceled` 또는 `sql.ErrTxDone`일 수 있다. 같은 Tx에서 Commit을 재시도하지 말고 새 connection으로 business key의 최종 상태를 확인한다.

### 10.1 raw BEGIN을 꼭 써야 한다면

다음 코드는 잘못됐다. `sql.DB`의 각 호출이 서로 다른 물리 connection으로 갈 수 있다.

```go
db.ExecContext(ctx, "BEGIN")
db.ExecContext(ctx, "INSERT INTO ACCOUNT VALUES (?, ?, ?)", 1, amount, "bad")
db.ExecContext(ctx, "COMMIT")
```

불가피하면 하나의 `*sql.Conn`을 고정한다.

```go
conn, err := db.Conn(ctx)
if err != nil {
    return err
}
defer conn.Close()

if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
    return err
}
// 같은 conn으로 DML과 COMMIT/ROLLBACK 실행
```

raw control SQL은 plain `BEGIN`, `COMMIT`, `ROLLBACK`만 사용한다. 중첩 BEGIN은 거부되고 transaction 밖 COMMIT/ROLLBACK은 no-op이다. statement 하나의 오류가 transaction 전체를 자동 종료하지는 않으므로 애플리케이션이 명시적으로 rollback해야 한다.

driver의 pool reset rollback은 반환된 connection에 남은 transaction이 다음 사용자에게 새는 것을 막는 안전망이다. 정상 transaction 종료 API가 아니다.

## 11. Appender와 transaction의 관계

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
fmt.Printf("append success=%d failed=%d\n", success, failed)
```

중요한 경계는 다음과 같다.

- Transaction table appender는 `sql.Tx`에 참여하지 않는다.
- appender batch 하나는 독립 statement transaction으로 처리되고 commit된다.
- 한 batch의 constraint 실패는 해당 batch에서 atomic reject되지만 여러 flush/batch 전체가 하나의 transaction은 아니다.
- `Flush()`, 자동 batch flush, `Close()`로 commit된 행은 나중의 `sql.Tx.Rollback()`으로 되돌릴 수 없다.
- `Append()`가 오류를 반환해도 `Close()`를 호출하고 success/failed/error를 모두 확인한다.
- appender 결과는 `RowsAffected()`가 아니라 `Close()`의 success/failed count로 판단한다.

`WithAppenderBuffer()`는 batch row 수 설정이 아니다. batch 경계가 필요하면 `WithBatchMaxRows()`, `WithBatchMaxBytes()`, `WithBatchMaxDelay()`를 사용한다. 위 한-row sample은 batch별 부분 commit을 재현하는 예제가 아니며, 여러 batch의 atomicity 경계는 회귀 테스트와 별도 실패 주입 시나리오로 확인한다.

## 12. prepared statement와 DDL 이후 metadata

CMI 4.0.3에서는 statement cache와 명시적 native/driver prepared statement를 실행하기 전에 같은 statement ID로 re-prepare할 수 있다. 목적은 DDL 뒤 과거 result metadata로 새 payload를 해석하는 silent corruption을 막는 것이다.

회귀 테스트가 강하게 입증한 시나리오는 다음과 같다.

1. 별도 admin connection으로 `(ID INTEGER, VALUE INTEGER)` table을 만든다.
2. 실행 connection에서 같은 SELECT를 준비하고 `301`을 조회해 warm-up한다.
3. admin connection이 table을 DROP하고 `VALUE VARCHAR(40)`으로 다시 만든다.
4. 같은 statement로 조회했을 때 `"after-ddl"`을 정확히 받는다.

```go
stmt, err := db.PrepareContext(ctx,
    "SELECT VALUE FROM CONFIG_TABLE WHERE ID=:id")
if err != nil {
    return err
}
defer stmt.Close()

var before int32
err = stmt.QueryRowContext(ctx, sql.Named("id", int32(1))).Scan(&before)
// 다른 connection에서 DROP/CREATE: INTEGER -> VARCHAR
var after string
err = stmt.QueryRowContext(ctx, sql.Named("id", int32(1))).Scan(&after)
```

현재 `51_named_reprepare_matrix.tc`는 native/database/sql의 `Query`와 `QueryRow`에서 **result metadata** 변경을 검증한다. cache `on`은 same-ID refresh 경로이고 cache `off`는 매번 fresh prepare하는 대조군이며, 명시적 prepared statement도 same-ID refresh를 검증한다. parameter column type, column 수/순서, nullable, DECIMAL precision/scale 변경까지 독립 검증했다고 확대해서는 안 된다.

Machbase 8.5.2 연결에는 이 CMI 4.0.3 same-ID re-prepare를 보내지 않고 legacy prepared execute 경로를 유지한다.

## 13. primary flag의 의미

client는 CMI primary flag를 decode할 수 있지만 현재 서버의 SELECT result metadata 생성 경로는 그 flag를 보내지 않는다. 따라서 `api.Column.PrimaryKey == false`는 “PK가 아님”의 확정 판정이 아니라 “result metadata에서 PK 정보가 전달되지 않음”일 수 있다.

PK 기반 UI나 upsert 판단을 result column bool 하나에 의존하지 말고 시스템 catalog SQL로 metadata를 조회한다. 현재 client의 query/bind/scan 동작은 primary flag에 의존하지 않으므로 flag 미전송 자체가 데이터 처리의 하위 호환성을 깨지는 않는다.

## 14. Machbase 8.5.2 하위 호환

client는 server CMI version을 보고 신규 packet과 metadata 사용을 제한한다.

| 기능 | CMI 4.0.3 main | Machbase 8.5.2 |
|---|---|---|
| positional query/bind/fetch/append | 지원 | 기존 동작 유지 |
| 기존 type metadata | 신규 layout 처리 | legacy layout 처리 |
| DECIMAL 신규 경로 | 독립 검증 | 이번 suite에서 독립 검증 안 함 |
| named bind | 지원 | 미지원 |
| nullable metadata | 3상태 | unknown (`ok=false`) |
| parameter ordinal | 256까지 검증 | legacy 255 성공, 256 거부 |
| same-ID re-prepare | result 변경 검증 | 보내지 않음 |

8.5.2 named 실패는 두 종류를 구분해야 한다.

```go
// 1) 8.5.2 SQL parser가 colon marker 문법 자체를 거부한다.
db.ExecContext(ctx, "SELECT * FROM T WHERE ID=:id", sql.Named("id", 1))

// 2) legacy에서 유효한 ? SQL에 named API를 주면 client capability gate가 거부한다.
db.ExecContext(ctx, "SELECT * FROM T WHERE ID=?", sql.Named("id", 1))
```

구버전에서 정상적으로 사용할 경로는 `?` SQL과 positional argument다.

```go
db.QueryRowContext(ctx, "SELECT NAME FROM T WHERE ID=?", int32(1))
```

## 15. 검증 범위와 아직 비어 있는 조합

nfx `test/regress/golang/3932-go-upgrade`의 15개 `.tc`가 기능별 독립 Go package를 실행한다.

| 범위 | 회귀 테스트 |
|---|---|
| smoke, appender, commit/rollback | `10_latest_features.tc` |
| Transaction catalog type | `11_transaction_catalog.tc` |
| 8.5.2 기본 호환 | `20_legacy_852_compat.tc` |
| DECIMAL 경계·변환·overflow | `30_decimal_boundary.tc`, `31_decimal_conversion_matrix.tc` |
| nullable metadata와 destination | `40_nullable_roundtrip.tc`, `41_null_destination_matrix.tc` |
| named bind와 DDL result refresh | `50_named_bind_matrix.tc`, `51_named_reprepare_matrix.tc` |
| transaction/pool 상태 | `60_transaction_semantics.tc`, `61_transaction_pool_matrix.tc` |
| ordinal 255/256/257 | `70_param_v2_ordinal.tc`, `71_param_v2_positional_matrix.tc` |
| 8.5.2 feature gate | `80_legacy_feature_gates.tc`, `81_legacy_named_gate_matrix.tc` |

현재 문서 감사에서 확인한 추가 결함 탐색 후보도 숨기지 않는다.

- SQL `NUMERIC` DDL은 DECIMAL과 별도로 아직 검증하지 않았다.
- typed nil pointer bind의 native/driver, direct/prepared 조합은 추가 검증이 필요하다.
- 256번째 parameter의 VARCHAR/BINARY/NULL/DECIMAL alignment 조합이 비어 있다.
- DDL re-prepare의 parameter metadata, column 수/순서, nullable, DECIMAL shape 변경 조합이 비어 있다.

이 목록은 미지원 선언이 아니라 현재 회귀 근거의 경계를 나타낸다.

## 16. 빠른 문제 해결

### `api.Decimal` 또는 `api.Named`이 undefined

```bash
go list -m github.com/machbase/neo-client
```

정식 지원 release 전이라면 `2-machbase-860-upgrade` checkout 또는 검증한 정확한 commit이 연결됐는지 확인한다.

### 8.5.2에서 `:id`가 syntax error

정상적인 legacy parser 동작이다. `?`와 positional argument를 사용한다.

### NULL이 0 또는 빈 문자열과 구분되지 않음

plain destination 대신 `sql.Null*`를 사용하고 항상 `Valid`를 먼저 확인한다. DECIMAL은 `database/sql`에서 `sql.NullString`으로 받은 뒤 선언 shape로 parse하는 패턴을 권장한다.

### transaction commit이 resource busy

Tx에서 연 `Rows`가 모두 닫혔는지, iteration 후 `Rows.Err()`를 확인했는지 점검한다.

### DDL 뒤 같은 statement 결과가 이상함

server와 client가 모두 CMI 4.0.3인지, 실제로 신규 branch/release를 사용 중인지 확인한다. 8.5.2에는 same-ID re-prepare가 활성화되지 않는다.

## 관련 이슈

- [neo-client #2](https://github.com/machbase/neo-client/issues/2)
- [dbms-nfx #3932](https://github.com/machbase/dbms-nfx/issues/3932)
