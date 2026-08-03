//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

/*
#cgo darwin CFLAGS: -DSQLITE_HAS_CODEC -I${SRCDIR}/../../../../../apps/desktop/resources/sqlcipher/darwin-arm64/include
#cgo darwin LDFLAGS: ${SRCDIR}/../../../../../apps/desktop/resources/sqlcipher/darwin-arm64/lib/libsqlite3.a -framework CoreFoundation -framework Security
#cgo windows CFLAGS: -DSQLITE_HAS_CODEC -I${SRCDIR}/../../../../../apps/desktop/resources/sqlcipher/win32-x64/include
#cgo windows LDFLAGS: ${SRCDIR}/../../../../../apps/desktop/resources/sqlcipher/win32-x64/lib/libsqlite3.a ${SRCDIR}/../../../../../apps/desktop/resources/sqlcipher/win32-x64/lib/libcrypto.a -lcrypt32 -luser32 -lws2_32
#include <sqlite3.h>
#include <stdlib.h>
#include <string.h>

int sqlite3_key(sqlite3*, const void*, int);
const char *sqlcipher_version(void);

static int tammy_bind_text(sqlite3_stmt *statement, int index, const char *value, int length) {
  return sqlite3_bind_text(statement, index, value, length, SQLITE_TRANSIENT);
}

static int tammy_bind_blob(sqlite3_stmt *statement, int index, const void *value, int length) {
  return sqlite3_bind_blob(statement, index, value, length, SQLITE_TRANSIENT);
}

static int tammy_prepare(sqlite3 *database, const char *query, int length, sqlite3_stmt **statement) {
  const char *tail = NULL;
  int result = sqlite3_prepare_v2(database, query, length, statement, &tail);
  if(result != SQLITE_OK) return result;
  while(tail != NULL && *tail != 0) {
    if(*tail != ' ' && *tail != '\t' && *tail != '\r' && *tail != '\n' && *tail != ';') {
      sqlite3_finalize(*statement);
      *statement = NULL;
      return SQLITE_MISUSE;
    }
    tail++;
  }
  return SQLITE_OK;
}

static void tammy_zero(void *value, int length) {
  if(value != NULL && length > 0) {
    volatile unsigned char *bytes = (volatile unsigned char *)value;
    while(length-- > 0) *bytes++ = 0;
  }
}
*/
import "C"

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var linkedLibrarySHA256 string

// RuntimeReport is authenticated evidence from the linked SQLCipher archive.
type RuntimeReport struct {
	LibrarySHA256          string `json:"library_sha256"`
	OrdinarySQLiteFallback bool   `json:"ordinary_sqlite_fallback"`
	RuntimeVersion         string `json:"runtime_version"`
	Version                string `json:"version"`
}

// Report returns the linked cipher version and build-authenticated archive hash.
func Report() (RuntimeReport, error) {
	runtimeVersion := C.GoString(C.sqlcipher_version())
	if runtimeVersion != PinnedVersion || len(linkedLibrarySHA256) != 64 {
		return RuntimeReport{}, errors.New("sqlcipher: linked library provenance unavailable")
	}
	for _, character := range linkedLibrarySHA256 {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return RuntimeReport{}, errors.New("sqlcipher: linked library provenance unavailable")
		}
	}
	return RuntimeReport{
		LibrarySHA256:          linkedLibrarySHA256,
		OrdinarySQLiteFallback: false,
		RuntimeVersion:         runtimeVersion,
		Version:                ReleaseVersion,
	}, nil
}

var (
	errDriverClosed       = errors.New("sqlcipher: connection is closed")
	errMultipleStatements = errors.New("sqlcipher: multiple statements are forbidden")
	errReadOnlyTx         = errors.New("sqlcipher: read-only transactions are unsupported")
	errIsolation          = errors.New("sqlcipher: transaction isolation is unsupported")
)

type sqlcipherDriver struct {
	connector *connector
}

func (driverInstance *sqlcipherDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("sqlcipher: DSN opens are forbidden; use the authenticated connector")
}

type connector struct {
	key  []byte
	mu   sync.Mutex
	path string
}

func (item *connector) Connect(ctx context.Context) (_ driver.Conn, returnedError error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item.mu.Lock()
	if len(item.key) != KeySize {
		item.mu.Unlock()
		return nil, ErrKeyRequired
	}
	key := append([]byte(nil), item.key...)
	item.mu.Unlock()
	defer zeroBytes(key)

	pathValue := C.CString(item.path)
	defer C.free(unsafe.Pointer(pathValue))
	var database *C.sqlite3
	flags := C.int(C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX | C.SQLITE_OPEN_NOFOLLOW)
	if result := C.sqlite3_open_v2(pathValue, &database, flags, nil); result != C.SQLITE_OK {
		if database != nil {
			message := sqliteMessage(database, result)
			C.sqlite3_close_v2(database)
			return nil, message
		}
		return nil, sqliteCodeError(result)
	}
	connection := &sqliteConn{database: database}
	defer func() {
		if returnedError != nil {
			_ = connection.Close()
		}
	}()

	keyValue := C.CBytes(key)
	if keyValue == nil {
		return nil, errors.New("sqlcipher: key allocation failed")
	}
	defer func() {
		C.tammy_zero(keyValue, C.int(len(key)))
		C.free(keyValue)
	}()
	if result := C.sqlite3_key(database, keyValue, C.int(len(key))); result != C.SQLITE_OK {
		return nil, sqliteMessage(database, result)
	}
	if err := connection.initialize(ctx); err != nil {
		return nil, err
	}
	return connection, nil
}

func (item *connector) Driver() driver.Driver {
	return &sqlcipherDriver{connector: item}
}

func (item *connector) destroy() {
	item.mu.Lock()
	defer item.mu.Unlock()
	zeroBytes(item.key)
	item.key = nil
}

type sqliteConn struct {
	database *C.sqlite3
	mu       sync.Mutex
}

func (connection *sqliteConn) initialize(ctx context.Context) error {
	if _, err := connection.scalarInt64(ctx, "SELECT count(*) FROM sqlite_schema"); err != nil {
		return errors.New("sqlcipher: key validation failed")
	}
	version, err := connection.scalarString(ctx, "PRAGMA cipher_version")
	if err != nil {
		return errors.New("sqlcipher: pinned cipher unavailable")
	}
	if version != PinnedVersion {
		return fmt.Errorf("sqlcipher: linked cipher version %q is not pinned %q", version, PinnedVersion)
	}
	for _, statement := range []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA secure_delete=ON",
		fmt.Sprintf("PRAGMA busy_timeout=%d", BusyTimeoutMilliseconds),
		"PRAGMA cipher_memory_security=ON",
	} {
		if _, err := connection.ExecContext(ctx, statement, nil); err != nil {
			return errors.New("sqlcipher: connection policy failed")
		}
	}
	journalMode, err := connection.scalarString(ctx, "PRAGMA journal_mode=WAL")
	if err != nil || !strings.EqualFold(journalMode, "wal") {
		return errors.New("sqlcipher: WAL policy failed")
	}
	for query, expected := range map[string]int64{
		"PRAGMA foreign_keys":  1,
		"PRAGMA secure_delete": 1,
		"PRAGMA busy_timeout":  BusyTimeoutMilliseconds,
	} {
		actual, queryErr := connection.scalarInt64(ctx, query)
		if queryErr != nil || actual != expected {
			return errors.New("sqlcipher: connection policy verification failed")
		}
	}
	return nil
}

func (connection *sqliteConn) Prepare(query string) (driver.Stmt, error) {
	return connection.PrepareContext(context.Background(), query)
}

func (connection *sqliteConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.IndexByte(query, 0) >= 0 || strings.TrimSpace(query) == "" {
		return nil, errors.New("sqlcipher: invalid statement")
	}
	connection.mu.Lock()
	database := connection.database
	connection.mu.Unlock()
	if database == nil {
		return nil, errDriverClosed
	}
	queryValue := C.CString(query)
	defer C.free(unsafe.Pointer(queryValue))
	var statement *C.sqlite3_stmt
	result := C.tammy_prepare(database, queryValue, C.int(len(query)), &statement)
	if result != C.SQLITE_OK {
		if result == C.SQLITE_MISUSE {
			return nil, errMultipleStatements
		}
		return nil, sqliteMessage(database, result)
	}
	if statement == nil {
		return nil, errors.New("sqlcipher: empty statement")
	}
	return &sqliteStmt{connection: connection, statement: statement}, nil
}

func (connection *sqliteConn) Close() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.database == nil {
		return nil
	}
	result := C.sqlite3_close_v2(connection.database)
	if result != C.SQLITE_OK {
		return sqliteMessage(connection.database, result)
	}
	connection.database = nil
	return nil
}

func (connection *sqliteConn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}

func (connection *sqliteConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if options.ReadOnly {
		return nil, errReadOnlyTx
	}
	statement := "BEGIN"
	switch options.Isolation {
	case driver.IsolationLevel(0):
	case driver.IsolationLevel(6):
		statement = "BEGIN IMMEDIATE"
	default:
		return nil, errIsolation
	}
	if _, err := connection.ExecContext(ctx, statement, nil); err != nil {
		return nil, err
	}
	return &sqliteTx{connection: connection}, nil
}

func (connection *sqliteConn) Ping(ctx context.Context) error {
	value, err := connection.scalarInt64(ctx, "SELECT 1")
	if err != nil {
		return err
	}
	if value != 1 {
		return errors.New("sqlcipher: ping failed")
	}
	return nil
}

func (connection *sqliteConn) ExecContext(ctx context.Context, query string, values []driver.NamedValue) (driver.Result, error) {
	statement, err := connection.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer statement.Close()
	return statement.(driver.StmtExecContext).ExecContext(ctx, values)
}

func (connection *sqliteConn) QueryContext(ctx context.Context, query string, values []driver.NamedValue) (driver.Rows, error) {
	prepared, err := connection.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	statement := prepared.(*sqliteStmt)
	rows, err := statement.query(ctx, values, true)
	if err != nil {
		_ = statement.Close()
		return nil, err
	}
	return rows, nil
}

func (connection *sqliteConn) scalarInt64(ctx context.Context, query string) (int64, error) {
	rows, err := connection.QueryContext(ctx, query, nil)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	values := make([]driver.Value, len(rows.Columns()))
	if len(values) != 1 {
		return 0, errors.New("sqlcipher: scalar shape invalid")
	}
	if err := rows.Next(values); err != nil {
		return 0, err
	}
	value, ok := values[0].(int64)
	if !ok {
		return 0, errors.New("sqlcipher: scalar type invalid")
	}
	return value, nil
}

func (connection *sqliteConn) scalarString(ctx context.Context, query string) (string, error) {
	rows, err := connection.QueryContext(ctx, query, nil)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	values := make([]driver.Value, len(rows.Columns()))
	if len(values) != 1 {
		return "", errors.New("sqlcipher: scalar shape invalid")
	}
	if err := rows.Next(values); err != nil {
		return "", err
	}
	value, ok := values[0].(string)
	if !ok {
		return "", errors.New("sqlcipher: scalar type invalid")
	}
	return value, nil
}

type sqliteStmt struct {
	connection *sqliteConn
	mu         sync.Mutex
	statement  *C.sqlite3_stmt
}

func (statement *sqliteStmt) Close() error {
	statement.mu.Lock()
	defer statement.mu.Unlock()
	if statement.statement == nil {
		return nil
	}
	result := C.sqlite3_finalize(statement.statement)
	statement.statement = nil
	if result != C.SQLITE_OK {
		return sqliteMessage(statement.connection.database, result)
	}
	return nil
}

func (statement *sqliteStmt) NumInput() int {
	statement.mu.Lock()
	defer statement.mu.Unlock()
	if statement.statement == nil {
		return -1
	}
	return int(C.sqlite3_bind_parameter_count(statement.statement))
}

func (statement *sqliteStmt) Exec(values []driver.Value) (driver.Result, error) {
	return statement.ExecContext(context.Background(), namedValues(values))
}

func (statement *sqliteStmt) Query(values []driver.Value) (driver.Rows, error) {
	return statement.QueryContext(context.Background(), namedValues(values))
}

func (statement *sqliteStmt) ExecContext(ctx context.Context, values []driver.NamedValue) (driver.Result, error) {
	statement.mu.Lock()
	defer statement.mu.Unlock()
	if statement.statement == nil {
		return nil, errDriverClosed
	}
	if err := statement.bind(values); err != nil {
		return nil, err
	}
	var result C.int
	err := interruptible(ctx, statement.connection.database, func() error {
		for {
			result = C.sqlite3_step(statement.statement)
			if result != C.SQLITE_ROW {
				break
			}
		}
		if result != C.SQLITE_DONE {
			return sqliteMessage(statement.connection.database, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	lastID := int64(C.sqlite3_last_insert_rowid(statement.connection.database))
	changes := int64(C.sqlite3_changes64(statement.connection.database))
	if reset := C.sqlite3_reset(statement.statement); reset != C.SQLITE_OK {
		return nil, sqliteMessage(statement.connection.database, reset)
	}
	return sqliteResult{changes: changes, lastInsertID: lastID}, nil
}

func (statement *sqliteStmt) QueryContext(ctx context.Context, values []driver.NamedValue) (driver.Rows, error) {
	return statement.query(ctx, values, false)
}

func (statement *sqliteStmt) query(ctx context.Context, values []driver.NamedValue, finalize bool) (driver.Rows, error) {
	statement.mu.Lock()
	defer statement.mu.Unlock()
	if statement.statement == nil {
		return nil, errDriverClosed
	}
	if err := statement.bind(values); err != nil {
		return nil, err
	}
	columnCount := int(C.sqlite3_column_count(statement.statement))
	columns := make([]string, columnCount)
	for index := range columns {
		columns[index] = C.GoString(C.sqlite3_column_name(statement.statement, C.int(index)))
	}
	return &sqliteRows{columns: columns, context: ctx, finalize: finalize, statement: statement}, nil
}

func (statement *sqliteStmt) bind(values []driver.NamedValue) error {
	if reset := C.sqlite3_reset(statement.statement); reset != C.SQLITE_OK {
		return sqliteMessage(statement.connection.database, reset)
	}
	if cleared := C.sqlite3_clear_bindings(statement.statement); cleared != C.SQLITE_OK {
		return sqliteMessage(statement.connection.database, cleared)
	}
	if len(values) != statement.NumInputUnlocked() {
		return fmt.Errorf("sqlcipher: received %d arguments, want %d", len(values), statement.NumInputUnlocked())
	}
	for _, value := range values {
		index := value.Ordinal
		if value.Name != "" {
			name := C.CString(":" + value.Name)
			index = int(C.sqlite3_bind_parameter_index(statement.statement, name))
			C.free(unsafe.Pointer(name))
			if index == 0 {
				return errors.New("sqlcipher: named parameter not found")
			}
		}
		if err := statement.bindValue(index, value.Value); err != nil {
			return err
		}
	}
	return nil
}

func (statement *sqliteStmt) NumInputUnlocked() int {
	return int(C.sqlite3_bind_parameter_count(statement.statement))
}

func (statement *sqliteStmt) bindValue(index int, value any) error {
	var result C.int
	switch typed := value.(type) {
	case nil:
		result = C.sqlite3_bind_null(statement.statement, C.int(index))
	case int64:
		result = C.sqlite3_bind_int64(statement.statement, C.int(index), C.sqlite3_int64(typed))
	case float64:
		result = C.sqlite3_bind_double(statement.statement, C.int(index), C.double(typed))
	case bool:
		integer := 0
		if typed {
			integer = 1
		}
		result = C.sqlite3_bind_int64(statement.statement, C.int(index), C.sqlite3_int64(integer))
	case string:
		text := C.CString(typed)
		result = C.tammy_bind_text(statement.statement, C.int(index), text, C.int(len(typed)))
		C.free(unsafe.Pointer(text))
	case []byte:
		if len(typed) == 0 {
			result = C.sqlite3_bind_zeroblob(statement.statement, C.int(index), 0)
		} else {
			blob := C.CBytes(typed)
			defer C.free(blob)
			result = C.tammy_bind_blob(statement.statement, C.int(index), blob, C.int(len(typed)))
		}
	case time.Time:
		formatted := typed.UTC().Format(time.RFC3339Nano)
		text := C.CString(formatted)
		result = C.tammy_bind_text(statement.statement, C.int(index), text, C.int(len(formatted)))
		C.free(unsafe.Pointer(text))
	default:
		return fmt.Errorf("sqlcipher: unsupported parameter type %T", value)
	}
	if result != C.SQLITE_OK {
		return sqliteMessage(statement.connection.database, result)
	}
	return nil
}

type sqliteRows struct {
	columns   []string
	context   context.Context
	closed    bool
	finalize  bool
	statement *sqliteStmt
}

func (rows *sqliteRows) Columns() []string {
	return append([]string(nil), rows.columns...)
}

func (rows *sqliteRows) Close() error {
	if rows.closed {
		return nil
	}
	rows.closed = true
	if rows.finalize {
		return rows.statement.Close()
	}
	rows.statement.mu.Lock()
	defer rows.statement.mu.Unlock()
	if rows.statement.statement == nil {
		return nil
	}
	result := C.sqlite3_reset(rows.statement.statement)
	if result != C.SQLITE_OK {
		return sqliteMessage(rows.statement.connection.database, result)
	}
	return nil
}

func (rows *sqliteRows) Next(destination []driver.Value) error {
	if rows.closed {
		return io.EOF
	}
	rows.statement.mu.Lock()
	defer rows.statement.mu.Unlock()
	if len(destination) != len(rows.columns) || rows.statement.statement == nil {
		return errors.New("sqlcipher: row destination invalid")
	}
	var result C.int
	err := interruptible(rows.context, rows.statement.connection.database, func() error {
		result = C.sqlite3_step(rows.statement.statement)
		return nil
	})
	if err != nil {
		return err
	}
	if result == C.SQLITE_DONE {
		return io.EOF
	}
	if result != C.SQLITE_ROW {
		return sqliteMessage(rows.statement.connection.database, result)
	}
	for index := range destination {
		column := C.int(index)
		switch C.sqlite3_column_type(rows.statement.statement, column) {
		case C.SQLITE_INTEGER:
			destination[index] = int64(C.sqlite3_column_int64(rows.statement.statement, column))
		case C.SQLITE_FLOAT:
			destination[index] = float64(C.sqlite3_column_double(rows.statement.statement, column))
		case C.SQLITE_TEXT:
			length := C.sqlite3_column_bytes(rows.statement.statement, column)
			pointer := C.sqlite3_column_text(rows.statement.statement, column)
			destination[index] = C.GoStringN((*C.char)(unsafe.Pointer(pointer)), length)
		case C.SQLITE_BLOB:
			length := C.sqlite3_column_bytes(rows.statement.statement, column)
			pointer := C.sqlite3_column_blob(rows.statement.statement, column)
			if length == 0 {
				destination[index] = []byte{}
			} else {
				destination[index] = C.GoBytes(pointer, length)
			}
		case C.SQLITE_NULL:
			destination[index] = nil
		default:
			return errors.New("sqlcipher: unsupported column type")
		}
	}
	return nil
}

type sqliteTx struct {
	connection *sqliteConn
	done       bool
}

func (transaction *sqliteTx) Commit() error {
	if transaction.done {
		return errors.New("sqlcipher: transaction is complete")
	}
	transaction.done = true
	_, err := transaction.connection.ExecContext(context.Background(), "COMMIT", nil)
	return err
}

func (transaction *sqliteTx) Rollback() error {
	if transaction.done {
		return errors.New("sqlcipher: transaction is complete")
	}
	transaction.done = true
	_, err := transaction.connection.ExecContext(context.Background(), "ROLLBACK", nil)
	return err
}

type sqliteResult struct {
	changes      int64
	lastInsertID int64
}

func (result sqliteResult) LastInsertId() (int64, error) { return result.lastInsertID, nil }
func (result sqliteResult) RowsAffected() (int64, error) { return result.changes, nil }

func namedValues(values []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, len(values))
	for index, value := range values {
		named[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return named
}

func interruptible(ctx context.Context, database *C.sqlite3, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		C.sqlite3_interrupt(database)
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	if err := operation(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func sqliteMessage(database *C.sqlite3, code C.int) error {
	if database == nil {
		return sqliteCodeError(code)
	}
	return fmt.Errorf("sqlcipher: %s (code %d)", C.GoString(C.sqlite3_errmsg(database)), int(code))
}

func sqliteCodeError(code C.int) error {
	return fmt.Errorf("sqlcipher: SQLite error code %d", int(code))
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
	runtime.KeepAlive(value)
}

var (
	_ driver.Conn               = (*sqliteConn)(nil)
	_ driver.ConnBeginTx        = (*sqliteConn)(nil)
	_ driver.ConnPrepareContext = (*sqliteConn)(nil)
	_ driver.Connector          = (*connector)(nil)
	_ driver.ExecerContext      = (*sqliteConn)(nil)
	_ driver.Pinger             = (*sqliteConn)(nil)
	_ driver.QueryerContext     = (*sqliteConn)(nil)
	_ driver.StmtExecContext    = (*sqliteStmt)(nil)
	_ driver.StmtQueryContext   = (*sqliteStmt)(nil)
)
