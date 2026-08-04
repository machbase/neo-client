# Machbase 8.6 Go client 초보자 안내서

이 문서는 Machbase 8.6 main trunk의 CMI 4.0.3 기능을 처음 사용하는 Go 개발자를 위한 실습형 안내서다. 연결부터 테이블 생성, 입력, 조회, NULL, transaction, appender와 구버전 호환까지 순서대로 실행할 수 있다.

문서의 예제는 nfx `test/regress/golang/3932-go-upgrade` 회귀 테스트에서 검증한 API 사용법과 기대값을 간단한 학습용 코드로 옮긴 것이다.

## 1. 먼저 알아둘 용어

- **native API**: `machgo.Database`, `api.Conn`, `api.Decimal`을 직접 사용하는 API다. Machbase 고유 기능과 metadata를 가장 충실하게 사용할 수 있다.
- **database/sql API**: Go 표준 `database/sql`을 사용하는 API다. connection pool과 `sql.Tx`, `sql.Stmt`를 사용하려는 일반 애플리케이션에 적합하다.
- **CMI**: Go client와 Machbase server 사이의 통신 protocol이다. 신규 기능은 양쪽이 CMI 4.0.3 이상일 때만 활성화된다.
- **precision**: DECIMAL 전체 유효 숫자 개수다. `DECIMAL(10,2)`의 precision은 10이다.
- **scale**: 소수점 아래 숫자 개수다. `DECIMAL(10,2)`의 scale은 2다.

## 2. 실습 준비

예제의 기본 환경은 다음과 같다.

```text
server   127.0.0.1
port     5656
user     sys
password manager
Go       1.22 이상
```

서버 주소나 계정이 다르면 예제의 connection 설정을 변경한다. 외부 프로젝트에서 release된 client를 사용할 때는 다음과 같이 module을 추가한다.

```bash
go mod init machbase-example
go get github.com/machbase/neo-client@latest
```

기능 branch를 검증하는 동안에는 이 저장소 checkout을 `go.work` 또는 `replace`로 연결한다. nfx 회귀 실행은 다음 환경 변수를 사용한다.

```bash
export NEO_CLIENT_SOURCE=${HOME}/work/neo-client
```

## 3. 완전한 실행 예제

### 3.1 native API 예제

[native 전체 소스](./examples/machbase860-native/main.go)는 다음 기능을 한 번에 보여준다.

1. native connection 생성
2. Transaction table 생성
3. `api.Decimal` 생성과 named insert
4. repeated named parameter 조회
5. nullable value scan
6. Transaction table appender
7. `RowsAffected()` 확인

```bash
go run ./docs/examples/machbase860-native
```

예상 출력:

```text
insert affected=1
row id=1 amount=123456789012345678.901234567890 precision=30 scale=12 note=named insert
nullable amount.valid=false note.valid=false
append success=1 failed=0
update affected=1
```

### 3.2 database/sql 예제

[database/sql 전체 소스](./examples/machbase860-database-sql/main.go)는 다음 기능을 보여준다.

1. DSN 연결과 `PingContext()`
2. `sql.Named()` insert
3. DECIMAL exact string 조회
4. `sql.NullString` NULL 조회
5. `sql.Tx` rollback
6. named prepared statement 재사용

```bash
go run ./docs/examples/machbase860-database-sql
```

예상 출력:

```text
insert affected=1
amount=123.450000000000
nullable amount.valid=false note.valid=false
note after rollback=database/sql
```

두 프로그램은 학습용 테이블을 만들고 종료할 때 삭제한다. 운영 코드에서는 애플리케이션이 임의로 schema를 생성하거나 삭제하지 않도록 분리한다.

## 지원 기능

- Transaction table 조회, bind, append 및 `database/sql` transaction
- DECIMAL/NUMERIC의 정밀도 손실 없는 조회, bind, append
- native `api.Named()` 및 `database/sql`의 `sql.Named()`
- nullable unknown/no-nulls/nullable metadata와 안전한 NULL scan
- UPDATE/DELETE의 affected row count
- DDL 이후 cached/명시적 prepared statement metadata 갱신
- Machbase 8.5.2 positional protocol 하위 호환

## 연결

native API는 `machgo`를 사용한다.

```go
ctx := context.Background()
db, err := machgo.NewDatabase(&machgo.Config{Host: "127.0.0.1", Port: 5656})
if err != nil {
    panic(err)
}
defer db.Close()

conn, err := db.Connect(ctx, api.WithPassword("sys", "manager"))
if err != nil {
    panic(err)
}
defer conn.Close()
```

`database/sql`은 root package를 blank import해 driver를 등록한다.

```go
import (
    "database/sql"
    _ "github.com/machbase/neo-client"
)

db, err := sql.Open("machbase",
    "server=tcp://sys:manager@127.0.0.1:5656;statement_cache=auto")
```

## Transaction table

Transaction table은 catalog에서 `api.TableTypeTransaction` 값 `8`로 노출된다. native query/bind와 appender를 사용할 수 있고 `database/sql`에서는 명시적 transaction을 사용할 수 있다.

```go
appender, err := conn.Appender(ctx, "TX_TABLE")
if err != nil {
    panic(err)
}
if err := appender.Append(int32(1), amount, "first"); err != nil {
    panic(err)
}
success, fail, err := appender.Close()
```

Transaction table의 UPDATE/DELETE는 primary key를 기준으로 실행해야 한다.

## DECIMAL/NUMERIC 입력

native API에서는 `api.ParseDecimal()`로 `api.Decimal`을 생성해 bind하는 방식을 권장한다. precision은 `1~65`, scale은 `0~30`이고 scale은 precision보다 클 수 없다.

```go
amount, err := api.ParseDecimal("123456789012345678.901234567890", 30, 12)
if err != nil {
    panic(err)
}

result := conn.Exec(ctx,
    "INSERT INTO TX_TABLE (ID, AMOUNT) VALUES (?, ?)",
    int32(1), amount)
if err := result.Err(); err != nil {
    panic(err)
}
```

`api.Decimal`은 `float64`가 아니라 다음 고정소수점 정보를 보관한다.

```text
실제 값 = unscaled(big.Int) × 10^-scale
```

예를 들어 `api.ParseDecimal("123.45", 10, 2)`는 `unscaled=12345`, `precision=10`, `scale=2`가 된다. 입력 소수 자릿수가 scale보다 많으면 0.5를 0에서 멀어지는 방향으로 반올림하고 precision을 넘으면 오류를 반환한다.

query bind는 CMI의 `DECIMAL(65,30)` 28-byte carrier를 사용한다. 정수부가 36~65자리여서 carrier에 직접 들어가지 않는 유효한 `api.Decimal`은 client가 exact VARCHAR payload로 전환하고 서버가 대상 DECIMAL/NUMERIC의 precision과 scale로 변환한다. 두 경로 모두 `float64`를 거치지 않는다.

`database/sql`에도 같은 값을 그대로 전달할 수 있다.

```go
_, err = db.ExecContext(ctx,
    "INSERT INTO TX_TABLE (ID, AMOUNT) VALUES (?, ?)",
    int32(1), amount)
```

### 입력값 선택 기준

```go
// 권장: 문자열에서 정확한 고정소수점 값 생성
amount, err := api.ParseDecimal("0.30", 10, 2)

// 권장하지 않음: 0.1과 0.2가 binary float에서 정확하지 않다.
amountFloat := 0.1 + 0.2
```

`ParseDecimal()`은 입력을 target scale에 맞게 반올림한다.

```go
positive, _ := api.ParseDecimal("1.235", 10, 2)  // 1.24
negative, _ := api.ParseDecimal("-1.235", 10, 2) // -1.24
```

다음 입력은 오류다.

```go
_, err := api.ParseDecimal("1000", 3, 0) // precision overflow
_, err = api.ParseDecimal("1.0", 0, 0)   // precision은 1 이상
_, err = api.ParseDecimal("1.0", 10, 31) // scale은 최대 30
```

## DECIMAL/NUMERIC 조회와 출력

native API는 result metadata의 precision과 scale을 사용해 wire payload를 `api.Decimal`로 복원한다.

```go
var got api.Decimal
err := conn.QueryRow(ctx,
    "SELECT AMOUNT FROM TX_TABLE WHERE ID = ?", int32(1)).Scan(&got)
if err != nil {
    panic(err)
}

fmt.Println(got.String())
fmt.Printf("precision=%d scale=%d\n", got.Precision(), got.Scale())
```

예상 출력:

```text
123456789012345678.901234567890
precision=30 scale=12
```

`database/sql`의 DECIMAL result는 정밀도 손실이 없는 문자열로 반환된다.

```go
var got string
err := db.QueryRowContext(ctx,
    "SELECT AMOUNT FROM TX_TABLE WHERE ID = ?", int32(1)).Scan(&got)
fmt.Println(got)
```

`ColumnType.DecimalSize()`로 선언 precision과 scale을 확인할 수 있다.

## named bind parameter

CMI 4.0.3 server에서는 native `api.Named()`을 사용할 수 있다.

```go
result := conn.Exec(ctx,
    "INSERT INTO TX_TABLE (ID, AMOUNT) VALUES (:id, :amount)",
    api.Named("id", int32(1)),
    api.Named("amount", amount))
```

`database/sql`에서는 `sql.Named()`을 사용한다.

```go
_, err := db.ExecContext(ctx,
    "INSERT INTO TX_TABLE (ID, AMOUNT) VALUES (:id, :amount)",
    sql.Named("id", int32(1)),
    sql.Named("amount", amount))
```

- 이름은 대소문자를 구분한다.
- SQL에서 같은 이름이 반복되면 하나의 supplied value가 모든 occurrence에 적용된다.
- named와 positional parameter를 한 실행에서 혼합할 수 없다.
- 누락, 초과, 중복 supplied name은 오류를 반환한다.
- CMI V2 ordinal은 256번째 parameter까지 정확히 처리하며 257개는 서버 제한으로 거부된다.

argument 순서는 SQL marker 순서와 같을 필요가 없다. 이름으로 매핑한다.

```go
result := conn.Exec(ctx,
    "INSERT INTO T (ID, A, B) VALUES (:id, :a, :b)",
    api.Named("b", int32(20)),
    api.Named("id", int32(1)),
    api.Named("a", int32(10)))
```

같은 marker가 여러 번 나오면 값을 한 번만 전달한다.

```go
row := conn.QueryRow(ctx,
    "SELECT ID FROM T WHERE ID=:id OR PARENT_ID=:id",
    api.Named("id", int32(1)))
```

다음은 모두 오류다.

```go
// 대소문자가 다름: SQL은 :id인데 argument는 ID
api.Named("ID", int32(1))

// 같은 이름을 두 번 전달
api.Named("id", int32(1)), api.Named("id", int32(2))

// named와 positional 혼합
api.Named("id", int32(1)), int32(1)
```

문자열 literal, quoted identifier와 SQL comment 안의 `:name`은 parameter로 세지 않는다. 실제 SQL expression의 marker만 bind 대상이다.

## nullable metadata와 NULL scan

CMI 4.0.3 metadata는 다음 3상태를 제공한다.

- `api.NullabilityUnknown`: 서버에서 판정 정보를 제공하지 않음
- `api.NullabilityNoNulls`: NOT NULL
- `api.NullabilityNullable`: NULL 허용

native API에서 NULL 가능 destination은 nullable wrapper를 사용한다.

```go
var amount sql.Null[api.Decimal]
var note sql.NullString
err := conn.QueryRow(ctx,
    "SELECT AMOUNT, NOTE FROM TX_TABLE WHERE ID = ?", int32(1)).
    Scan(&amount, &note)
```

NULL을 일반 `api.Decimal`, 문자열 또는 숫자 destination에 scan하면 오류를 반환한다. nullable destination을 재사용할 때 NULL 행은 `Valid=false`가 되고 이전 payload도 0값으로 초기화된다. native `[]byte`와 `driver.Value` destination은 NULL에서 `nil`이 된다.

`database/sql`에서는 `ColumnType.Nullable()`로 metadata를 확인한다. 8.5.2처럼 nullable metadata가 없는 서버에서는 `ok=false`가 반환된다.

nullable destination은 반복문 밖에서 선언해 재사용해도 안전하다.

```go
var amount sql.Null[api.Decimal]
var note sql.NullString

for rows.Next() {
    if err := rows.Scan(&amount, &note); err != nil {
        panic(err)
    }
    if !amount.Valid {
        fmt.Println("AMOUNT is NULL")
        continue
    }
    fmt.Println(amount.V.String())
}
```

이전 행이 non-NULL이고 다음 행이 NULL이어도 `Valid=false`뿐 아니라 내부 값도 0값으로 초기화된다. NULL을 일반 `string`, 숫자 또는 `api.Decimal`에 scan해서 0으로 간주하지 않는다. 이 경우 client는 명시적으로 오류를 반환한다.

## transaction

`database/sql`의 기본 transaction을 지원한다.

```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    panic(err)
}

if _, err := tx.ExecContext(ctx,
    "UPDATE TX_TABLE SET AMOUNT=? WHERE ID=?", amount, int32(1)); err != nil {
    _ = tx.Rollback()
    panic(err)
}
if err := tx.Commit(); err != nil {
    panic(err)
}
```

- 기본 isolation만 지원한다.
- read-only 및 별도 isolation level은 오류를 반환한다.
- raw/prepared `BEGIN`, `COMMIT`, `ROLLBACK`도 connection 상태에 반영된다.
- 열린 transaction이 pool로 반환되면 다음 사용자에게 대여하기 전에 rollback된다.
- `LastInsertId()`는 지원하지 않으며 `RowsAffected()`를 사용한다.

transaction을 시작한 뒤 오류가 발생하면 반드시 rollback한다.

```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return err
}

if _, err := tx.ExecContext(ctx,
    "UPDATE TX_TABLE SET NOTE=? WHERE ID=?", "changed", int32(1)); err != nil {
    _ = tx.Rollback()
    return err
}

if err := tx.Commit(); err != nil {
    return err
}
```

`Commit()`이나 `Rollback()`이 끝난 `sql.Tx`는 재사용할 수 없으며 이후 호출은 `sql.ErrTxDone`을 반환한다.

## prepared statement와 DDL

CMI 4.0.3에서는 내부 statement cache와 native `Conn.Prepare()`/`database/sql` `PrepareContext()`로 생성한 명시적 statement를 실행하기 전에 같은 statement ID로 re-prepare한다. 다른 연결에서 테이블을 DROP/CREATE해 parameter 또는 result 컬럼 타입이 바뀌어도 이전 metadata로 새 payload를 해석하지 않는다.

이 동작은 client와 server가 모두 CMI 4.0.3 이상일 때만 활성화된다.

애플리케이션 코드는 기존 statement를 그대로 재사용하면 된다.

```go
stmt, err := db.PrepareContext(ctx,
    "SELECT VALUE FROM CONFIG_TABLE WHERE ID=:id")
if err != nil {
    panic(err)
}
defer stmt.Close()

var value string
err = stmt.QueryRowContext(ctx, sql.Named("id", int32(1))).Scan(&value)
```

다른 connection이 table을 DROP/CREATE해 result type을 INTEGER에서 VARCHAR로 바꾼 경우에도 CMI 4.0.3 client는 실행 전에 metadata를 다시 받는다. execute response에는 새 result metadata가 포함되지 않기 때문에 이 re-prepare가 없으면 과거 type으로 payload를 해석하는 silent corruption이 생길 수 있다.

## 8.5.2 하위 호환성

client는 connect response의 server CMI version으로 신규 기능을 제한한다.

| 기능 | CMI 4.0.3 | Machbase 8.5.2 |
|---|---|---|
| positional query/bind/fetch/append | 지원 | 기존 동작 유지 |
| DECIMAL 신규 compact metadata | 지원 | legacy layout 사용 |
| named bind | 지원 | unsupported 오류 |
| nullable metadata | 3상태 제공 | unknown (`ok=false`) |
| parameter ordinal | 최대 256 | legacy 최대 255 |
| 같은-ID re-prepare | 지원 | 실행하지 않음 |

## 현재 제한사항

- CMI에는 primary flag가 예약돼 있으나 현재 서버의 SELECT result metadata 생성 경로는 이 flag를 송신하지 않는다. primary key 정보는 catalog metadata API로 확인한다.
- Transaction table의 `LastInsertId()` wire 계약은 없다.
- named bind, nullable flags, 256 parameter 및 같은-ID re-prepare는 CMI 4.0.3 capability가 필요하다.

## 문제 해결

### 신규 API가 undefined로 compile됨

`api.Decimal`, `api.Named` 또는 `TableTypeTransaction`이 undefined라면 구버전 module을 사용하고 있는지 확인한다.

```bash
go list -m github.com/machbase/neo-client
```

nfx feature branch 회귀에서는 다음처럼 실행한다.

```bash
cd ${MACHBASEDEV_HOME}/test/regress/golang
NEO_CLIENT_SOURCE=${HOME}/work/neo-client itf golang.ts
```

### 8.5.2에서 named bind가 실패함

정상적인 capability gate다. 8.5.2에는 positional `?` parameter를 사용한다.

### NULL이 0 또는 빈 문자열과 구분되지 않음

일반 destination 대신 `sql.Null*` 또는 `sql.Null[T]`를 사용한다. `Valid`를 먼저 확인한 뒤 값을 읽는다.

## 회귀 테스트와 예제 대응표

| 학습 항목 | 회귀 테스트 |
|---|---|
| 통합 기능과 Transaction appender | `10_latest_features.tc` |
| Transaction catalog type | `11_transaction_catalog.tc` |
| DECIMAL 경계·반올림·overflow | `30_decimal_boundary.tc`, `31_decimal_conversion_matrix.tc` |
| nullable metadata와 stale value 제거 | `40_nullable_roundtrip.tc`, `41_null_destination_matrix.tc` |
| named bind 정상·오류·lexical 경계 | `50_named_bind_matrix.tc`, `51_named_reprepare_matrix.tc` |
| commit·rollback·pool reset | `60_transaction_semantics.tc`, `61_transaction_pool_matrix.tc` |
| parameter 255/256/257 경계 | `70_param_v2_ordinal.tc`, `71_param_v2_positional_matrix.tc` |
| Machbase 8.5.2 호환 | `20_legacy_852_compat.tc`, `80_legacy_feature_gates.tc`, `81_legacy_named_gate_matrix.tc` |

## 관련 이슈

- [neo-client #2](https://github.com/machbase/neo-client/issues/2)
- [dbms-nfx #3932](https://github.com/machbase/dbms-nfx/issues/3932)
