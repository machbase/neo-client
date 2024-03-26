// package machrpc is a refrence implementation of client
// that interwork with machbase-neo server via gRPC.
package machrpc

import (
	context "context"
	"database/sql"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/machbase/neo-engine/spi"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Client is a convenient data type represents client side of machbase-neo.
//
//	client := machrpc.NewClient(WithServer(serverAddr, "path/to/server_cert.pem"))
//
// serverAddr can be tcp://ipaddr:port or unix://path.
// The path of unix domain socket can be absolute/releative path.
type Client struct {
	conn grpc.ClientConnInterface
	cli  MachbaseClient

	serverAddr    string
	serverCert    string
	certPath      string
	keyPath       string
	queryTimeout  time.Duration
	appendTimeout time.Duration

	closeOnce sync.Once
}

// NewClient creates new instance of Client.
func NewClient(opts ...Option) (*Client, error) {
	client := &Client{
		queryTimeout:  0,
		appendTimeout: 3 * time.Second,
	}
	for _, o := range opts {
		o(client)
	}

	if client.serverAddr == "" {
		return nil, errors.New("server address is not specified")
	}

	var conn grpc.ClientConnInterface
	var err error

	if client.keyPath != "" && client.certPath != "" && client.serverCert != "" {
		conn, err = MakeGrpcTlsConn(client.serverAddr, client.keyPath, client.certPath, client.serverCert)
	} else {
		conn, err = MakeGrpcConn(client.serverAddr, nil)
	}

	if err != nil {
		return nil, errors.Wrap(err, "NewClient")
	}
	client.conn = conn
	client.cli = NewMachbaseClient(conn)

	return client, nil
}

func (client *Client) Close() {
	client.closeOnce.Do(func() {
		client.conn = nil
		client.cli = nil
	})
}

func (client *Client) UserAuth(user string, password string) (bool, error) {
	ctx, cancelFunc := client.queryContext()
	defer cancelFunc()
	req := &UserAuthRequest{LoginName: user, Password: password}
	rsp, err := client.cli.UserAuth(ctx, req)
	if err != nil {
		return false, err
	}
	if !rsp.Success {
		return false, errors.New(rsp.Reason)
	}
	return true, nil
}

// GetServerInfo invoke gRPC call to get ServerInfo
func (client *Client) GetServerInfo() (*ServerInfo, error) {
	ctx, cancelFunc := client.queryContext()
	defer cancelFunc()
	req := &ServerInfoRequest{}
	rsp, err := client.cli.GetServerInfo(ctx, req)
	if err != nil {
		return nil, err
	}
	if !rsp.Success {
		return nil, errors.New(rsp.Reason)
	}
	return rsp, nil
}

func (client *Client) GetServicePorts(svc string) ([]*Port, error) {
	ctx, cancelFunc := client.queryContext()
	defer cancelFunc()
	req := &ServicePortsRequest{Service: svc}
	rsp, err := client.cli.GetServicePorts(ctx, req)
	if err != nil {
		return nil, err
	}

	return rsp.Ports, nil
}

func (client *Client) ServerSessions(reqStatz, reqSessions bool) (*Statz, []*Session, error) {
	ctx, cancelFunc := client.queryContext()
	defer cancelFunc()
	req := &SessionsRequest{Statz: reqStatz, Sessions: reqSessions}
	rsp, err := client.cli.Sessions(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return rsp.Statz, rsp.Sessions, nil
}

func (client *Client) queryContext() (context.Context, context.CancelFunc) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.New(map[string]string{"client": "machrpc"}))
	cancel := func() {}
	if client.queryTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, client.queryTimeout)
	}
	return ctx, cancel
}

// Connect make a connection to the server
func (client *Client) Connect(ctx context.Context, opts ...spi.ConnectOption) (spi.Conn, error) {
	ret := &ClientConn{client: client}
	for _, o := range opts {
		o(ret)
	}

	req := &ConnRequest{
		User:     ret.dbUser,
		Password: ret.dbPassword,
	}
	rsp, err := client.cli.Conn(ctx, req)
	if err != nil {
		return nil, err
	}

	if !rsp.Success {
		return nil, errors.New(rsp.Reason)
	}
	ret.ctx = ctx
	ret.handle = rsp.Conn
	return ret, nil
}

type ClientConn struct {
	ctx    context.Context
	client *Client

	dbUser     string
	dbPassword string

	handle    *ConnHandle
	closeOnce sync.Once
}

var _ spi.Conn = &ClientConn{}

func (conn *ClientConn) Close() error {
	var err error
	conn.closeOnce.Do(func() {
		req := &ConnCloseRequest{Conn: conn.handle}
		_, err = conn.client.cli.ConnClose(conn.ctx, req)
	})
	return err
}

func (conn *ClientConn) Ping() (time.Duration, error) {
	tick := time.Now()
	req := &PingRequest{Conn: conn.handle, Token: tick.UnixNano()}
	rsp, err := conn.client.cli.Ping(conn.ctx, req)
	if err != nil {
		return time.Since(tick), err
	}
	if !rsp.Success {
		return time.Since(tick), errors.New(rsp.Reason)
	}
	return time.Since(tick), nil
}

// Explain retrieve execution plan of the given SQL statement.
func (conn *ClientConn) Explain(ctx context.Context, sqlText string, full bool) (string, error) {
	req := &ExplainRequest{Conn: conn.handle, Sql: sqlText, Full: full}
	rsp, err := conn.client.cli.Explain(ctx, req)
	if err != nil {
		return "", err
	}
	if !rsp.Success {
		return "", fmt.Errorf(rsp.Reason)
	}
	return rsp.Plan, nil
}

// Exec executes SQL statements that does not return result
// like 'ALTER', 'CREATE TABLE', 'DROP TABLE', ...
func (conn *ClientConn) Exec(ctx context.Context, sqlText string, params ...any) spi.Result {
	pbparams, err := ConvertAnyToPb(params)
	if err != nil {
		return &ExecResult{err: err}
	}
	req := &ExecRequest{Conn: conn.handle, Sql: sqlText, Params: pbparams}
	rsp, err := conn.client.cli.Exec(ctx, req)
	if err != nil {
		return &ExecResult{err: err}
	}
	if !rsp.Success {
		return &ExecResult{err: errors.New(rsp.Reason), message: rsp.Reason}
	}
	return &ExecResult{message: rsp.Reason, rowsAffected: rsp.RowsAffected}
}

type ExecResult struct {
	err          error
	rowsAffected int64
	message      string
}

func (r *ExecResult) Err() error {
	return r.err
}

func (r *ExecResult) RowsAffected() int64 {
	return r.rowsAffected
}

func (r *ExecResult) Message() string {
	return r.message
}

// Query executes SQL statements that are expected multipe rows as result.
// Commonly used to execute 'SELECT * FROM <TABLE>'
//
// Rows returned by QueryContext() must be closed to prevent leaking resources.
//
//	ctx, cancelFunc := context.WithTimeout(5*time.Second)
//	defer cancelFunc()
//
//	rows, err := client.Query(ctx, "select * from my_table where name = ?", my_name)
//	if err != nil {
//		panic(err)
//	}
//	defer rows.Close()
func (conn *ClientConn) Query(ctx context.Context, sqlText string, params ...any) (spi.Rows, error) {
	pbparams, err := ConvertAnyToPb(params)
	if err != nil {
		return nil, err
	}

	req := &QueryRequest{Conn: conn.handle, Sql: sqlText, Params: pbparams}
	rsp, err := conn.client.cli.Query(ctx, req)
	if err != nil {
		return nil, err
	}

	if rsp.Success {
		return &Rows{
			ctx:          ctx,
			client:       conn.client,
			rowsAffected: rsp.RowsAffected,
			message:      rsp.Reason,
			handle:       rsp.RowsHandle,
		}, nil
	} else {
		if len(rsp.Reason) > 0 {
			return nil, errors.New(rsp.Reason)
		}
		return nil, errors.New("unknown error")
	}
}

type Rows struct {
	ctx          context.Context
	client       *Client
	message      string
	rowsAffected int64
	handle       *RowsHandle
	values       []any
	err          error
	closeOnce    sync.Once
}

// Close release all resources that assigned to the Rows
func (rows *Rows) Close() error {
	var err error
	rows.closeOnce.Do(func() {
		_, err = rows.client.cli.RowsClose(rows.ctx, rows.handle)
	})
	return err
}

// IsFetchable returns true if statement that produced this Rows was fetch-able (e.g was select?)
func (rows *Rows) IsFetchable() bool {
	return rows.handle != nil
}

func (rows *Rows) Message() string {
	return rows.message
}

func (rows *Rows) RowsAffected() int64 {
	return rows.rowsAffected
}

// Columns returns list of column info that consists of result of query statement.
func (rows *Rows) Columns() (spi.Columns, error) {
	rsp, err := rows.client.cli.Columns(rows.ctx, rows.handle)
	if err != nil {
		return nil, err
	}
	if rsp.Success {
		return toSpiColumns(rsp.Columns), nil
	} else {
		if len(rsp.Reason) > 0 {
			return nil, errors.New(rsp.Reason)
		} else {
			return nil, fmt.Errorf("fail to get columns info")
		}
	}
}

func toSpiColumns(cols []*Column) spi.Columns {
	rt := make([]*spi.Column, len(cols))
	for i, c := range cols {
		rt[i] = &spi.Column{
			Name:   c.Name,
			Type:   c.Type,
			Size:   int(c.Size),
			Length: int(c.Length),
		}
	}
	return rt
}

// Next returns true if there are at least one more record that can be fetchable
// rows, _ := client.Query("select name, value from my_table")
//
//	for rows.Next(){
//		var name string
//		var value float64
//		rows.Scan(&name, &value)
//	}
func (rows *Rows) Next() bool {
	if rows.err != nil {
		return false
	}
	rsp, err := rows.client.cli.RowsFetch(rows.ctx, rows.handle)
	if err != nil {
		rows.err = err
		return false
	}
	if rsp.Success {
		if rsp.HasNoRows {
			return false
		}
		rows.values = ConvertPbToAny(rsp.Values)
	} else {
		if len(rsp.Reason) > 0 {
			rows.err = errors.New(rsp.Reason)
		}
		rows.values = nil
	}
	return !rsp.HasNoRows
}

// Scan retrieve values of columns
//
//	for rows.Next(){
//		var name string
//		var value float64
//		rows.Scan(&name, &value)
//	}
func (rows *Rows) Scan(cols ...any) error {
	if rows.err != nil {
		return rows.err
	}
	if rows.values == nil {
		return sql.ErrNoRows
	}
	return scan(rows.values, cols)
}

// QueryRow executes a SQL statement that expects a single row result.
//
//	var cnt int
//	row := client.QueryRow(ctx, "select count(*) from my_table where name = ?", "my_name")
//	row.Scan(&cnt)
func (conn *ClientConn) QueryRow(ctx context.Context, sqlText string, params ...any) spi.Row {
	pbparams, err := ConvertAnyToPb(params)
	if err != nil {
		return &Row{success: false, err: err}
	}

	req := &QueryRowRequest{Conn: conn.handle, Sql: sqlText, Params: pbparams}
	rsp, err := conn.client.cli.QueryRow(ctx, req)
	if err != nil {
		return &Row{success: false, err: err}
	}

	var row = &Row{}
	row.success = rsp.Success
	row.rowsAffected = rsp.RowsAffected
	if rsp.Message == "" {
		row.message = rsp.Reason
	} else {
		row.message = rsp.Message
	}
	row.err = nil
	if !rsp.Success && len(rsp.Reason) > 0 {
		row.err = errors.New(rsp.Reason)
	}
	row.values = ConvertPbToAny(rsp.Values)
	return row
}

type Row struct {
	success bool
	err     error
	values  []any

	rowsAffected int64
	message      string
}

func (row *Row) Success() bool {
	return row.success
}

func (row *Row) Err() error {
	return row.err
}

func (row *Row) Scan(cols ...any) error {
	if row.err != nil {
		return row.err
	}
	if !row.success {
		return sql.ErrNoRows
	}
	err := scan(row.values, cols)
	return err
}

func (row *Row) RowsAffected() int64 {
	return row.rowsAffected
}

func (row *Row) Message() string {
	return row.message
}

func (row *Row) Values() []any {
	return row.values
}

func scan(src []any, dst []any) error {
	for i := range dst {
		if i >= len(src) {
			return fmt.Errorf("column %d is out of range %d", i, len(src))
		}
		if src[i] == nil {
			dst[i] = nil
			continue
		}
		var err error
		switch v := src[i].(type) {
		default:
			return fmt.Errorf("column %d is %T, not compatible with %T", i, v, dst[i])
		case *int:
			err = ScanInt32(int32(*v), dst[i])
		case *int16:
			err = ScanInt16(*v, dst[i])
		case *int32:
			err = ScanInt32(*v, dst[i])
		case *int64:
			err = ScanInt64(*v, dst[i])
		case *time.Time:
			err = ScanDateTime(*v, dst[i])
		case *float32:
			err = ScanFloat32(*v, dst[i])
		case *float64:
			err = ScanFloat64(*v, dst[i])
		case *net.IP:
			err = ScanIP(*v, dst[i])
		case *string:
			err = ScanString(*v, dst[i])
		case []byte:
			err = ScanBytes(v, dst[i])
		case int:
			err = ScanInt32(int32(v), dst[i])
		case int16:
			err = ScanInt16(v, dst[i])
		case int32:
			err = ScanInt32(v, dst[i])
		case int64:
			err = ScanInt64(v, dst[i])
		case time.Time:
			err = ScanDateTime(v, dst[i])
		case float32:
			err = ScanFloat32(v, dst[i])
		case float64:
			err = ScanFloat64(v, dst[i])
		case net.IP:
			err = ScanIP(v, dst[i])
		case string:
			err = ScanString(v, dst[i])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Appender creates a new Appender for the given table.
// Appender should be closed otherwise it may cause server side resource leak.
//
//	app, _ := client.Appender(ctx, "MYTABLE")
//	defer app.Close()
//	app.Append("name", time.Now(), 3.14)
func (conn *ClientConn) Appender(ctx context.Context, tableName string, opts ...spi.AppenderOption) (spi.Appender, error) {

	ap := &Appender{
		ctx:             ctx,
		timeformat:      "ns",
		bufferThreshold: 400,
	}

	for _, opt := range opts {
		opt(ap)
	}

	openRsp, err := conn.client.cli.Appender(ctx, &AppenderRequest{
		Conn:       conn.handle,
		TableName:  tableName,
		Timeformat: ap.timeformat,
	})
	if err != nil {
		return nil, errors.Wrap(err, "Appender")
	}

	if !openRsp.Success {
		return nil, errors.New(openRsp.Reason)
	}

	appendClient, err := conn.client.cli.Append(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "AppendClient")
	}

	ap.client = conn.client
	ap.appendClient = appendClient
	ap.tableName = openRsp.TableName
	ap.tableType = spi.TableType(openRsp.TableType)
	ap.handle = openRsp.Handle

	ap.bufferTicker = time.NewTicker(time.Second)
	go func() {
		for range ap.bufferTicker.C {
			ap.flush(nil)
		}
	}()

	return ap, nil
}

func AppenderTimeformat(timeformat string) spi.AppenderOption {
	return func(a spi.Appender) {
		if apd, ok := a.(*Appender); ok {
			apd.timeformat = timeformat
		}
	}
}

func AppenderBufferThreshold(threshold int) spi.AppenderOption {
	return func(a spi.Appender) {
		if apd, ok := a.(*Appender); ok {
			apd.bufferThreshold = threshold
		}
	}
}

type Appender struct {
	ctx          context.Context
	client       *Client
	appendClient Machbase_AppendClient
	tableName    string
	tableType    spi.TableType
	handle       *AppenderHandle
	timeformat   string

	buffer       []*AppendRecord
	bufferLock   sync.Mutex
	bufferTicker *time.Ticker

	bufferThreshold int
}

// Close releases all resources that allocated to the Appender
func (appender *Appender) Close() (int64, int64, error) {
	if appender.appendClient == nil {
		return 0, 0, nil
	}

	if appender.bufferTicker != nil {
		appender.bufferTicker.Stop()
	}
	appender.flush(nil)

	client := appender.appendClient
	appender.appendClient = nil

	done, err := client.CloseAndRecv()
	if done != nil {
		return done.SuccessCount, done.FailCount, err
	} else {
		return 0, 0, err
	}
}

func (appender *Appender) TableName() string {
	return appender.tableName
}

func (appender *Appender) TableType() spi.TableType {
	return appender.tableType
}

func (appender *Appender) AppendWithTimestamp(ts time.Time, cols ...any) error {
	return appender.Append(append([]any{ts}, cols...))
}

// Append appends a new record of the table.
func (appender *Appender) Append(cols ...any) error {
	if appender.appendClient == nil {
		return sql.ErrTxDone
	}

	params, err := ConvertAnyToPbTuple(cols)
	if err != nil {
		return err
	}
	err = appender.flush(&AppendRecord{Tuple: params})
	return err
}

func (appender *Appender) Columns() (spi.Columns, error) {
	// TODO implements
	return nil, nil
}

// force flush if rec is nil
// allow buffering if rec is not nil
func (appender *Appender) flush(rec *AppendRecord) error {
	appender.bufferLock.Lock()
	defer appender.bufferLock.Unlock()

	if rec != nil {
		appender.buffer = append(appender.buffer, rec)
	}
	if len(appender.buffer) == 0 {
		return nil
	}

	if rec != nil && len(appender.buffer) < appender.bufferThreshold {
		// write new record, but not enough to flush to network
		return nil
	}

	err := appender.appendClient.Send(&AppendData{
		Handle:  appender.handle,
		Records: appender.buffer,
	})
	if err == nil {
		appender.buffer = appender.buffer[:0]
	}
	return err
}
