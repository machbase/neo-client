# Machbase 8.6 Go client 기능 안내

이 문서는 Machbase 8.6 main trunk의 CMI 4.0.3 기능을 사용하는 `neo-client`의 공개 API와 Machbase 8.5.2 하위 호환 범위를 설명한다.

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

## prepared statement와 DDL

CMI 4.0.3에서는 내부 statement cache와 native `Conn.Prepare()`/`database/sql` `PrepareContext()`로 생성한 명시적 statement를 실행하기 전에 같은 statement ID로 re-prepare한다. 다른 연결에서 테이블을 DROP/CREATE해 parameter 또는 result 컬럼 타입이 바뀌어도 이전 metadata로 새 payload를 해석하지 않는다.

이 동작은 client와 server가 모두 CMI 4.0.3 이상일 때만 활성화된다.

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

## 관련 이슈

- [neo-client #2](https://github.com/machbase/neo-client/issues/2)
- [dbms-nfx #3932](https://github.com/machbase/dbms-nfx/issues/3932)
