package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"dbpull/internal/config"

	mysql "github.com/go-sql-driver/mysql"
)

func TestBuildTargetDSN(t *testing.T) {
	dsn, err := buildTargetDSN(config.TargetConfig{
		Host:     "localhost",
		Port:     3306,
		Database: "olshoperp_local",
		Username: "root",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("buildTargetDSN() error = %v", err)
	}

	for _, want := range []string{
		"root:secret@tcp(localhost:3306)/olshoperp_local",
		"interpolateParams=true",
		"parseTime=true",
	} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn = %q, want substring %q", dsn, want)
		}
	}
}

func TestTargetPing(t *testing.T) {
	client := &TargetClient{
		session: fakeTargetSession{
			ping: func(context.Context) error { return nil },
		},
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestTargetSchemaMethods(t *testing.T) {
	var queries []string

	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				queries = append(queries, query)
				return fakeResult{}, nil
			},
		},
	}

	if err := client.DisableForeignKeyChecks(context.Background()); err != nil {
		t.Fatalf("DisableForeignKeyChecks() error = %v", err)
	}

	if err := client.DropTable(context.Background(), "users"); err != nil {
		t.Fatalf("DropTable() error = %v", err)
	}

	if err := client.CreateTable(context.Background(), "CREATE TABLE `users` (`id` bigint)"); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}

	if err := client.EnableForeignKeyChecks(context.Background()); err != nil {
		t.Fatalf("EnableForeignKeyChecks() error = %v", err)
	}

	want := []string{
		"SET FOREIGN_KEY_CHECKS = 0",
		"DROP TABLE IF EXISTS `users`",
		"CREATE TABLE `users` (`id` bigint)",
		"SET FOREIGN_KEY_CHECKS = 1",
	}
	for i, query := range want {
		if queries[i] != query {
			t.Fatalf("queries[%d] = %q, want %q", i, queries[i], query)
		}
	}
}

func TestTargetSchemaMethodsOpenSessionLazily(t *testing.T) {
	opened := false
	client := &TargetClient{
		newSes: func(ctx context.Context) (targetSession, func() error, error) {
			opened = true
			return fakeTargetSession{}, func() error { return nil }, nil
		},
	}

	if err := client.DropTable(context.Background(), "users"); err != nil {
		t.Fatalf("DropTable() error = %v", err)
	}
	if !opened {
		t.Fatal("target session was not opened")
	}
}

func TestInsertBatch(t *testing.T) {
	var gotQuery string
	var gotArgs []any

	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				gotQuery = query
				gotArgs = append([]any(nil), args...)
				return fakeResult{}, nil
			},
		},
	}

	err := client.InsertBatch(context.Background(), "users", RowBatch{
		Columns: []string{"id", "name"},
		Rows: [][]any{
			{int64(1), "alice"},
			{int64(2), "bob"},
		},
	})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}

	wantQuery := "INSERT INTO `users` (`id`, `name`) VALUES (?, ?), (?, ?)"
	if gotQuery != wantQuery {
		t.Fatalf("query = %q, want %q", gotQuery, wantQuery)
	}

	wantArgs := []any{int64(1), "alice", int64(2), "bob"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestInsertBatchPreservesEmptyBytesAndNil(t *testing.T) {
	var gotArgs []any

	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				gotArgs = append([]any(nil), args...)
				return fakeResult{}, nil
			},
		},
	}

	err := client.InsertBatch(context.Background(), "files", RowBatch{
		Columns: []string{"file", "note"},
		Rows: [][]any{
			{[]byte{}, nil},
		},
	})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}

	fileValue, ok := gotArgs[0].([]byte)
	if !ok {
		t.Fatalf("gotArgs[0] type = %T, want []byte", gotArgs[0])
	}
	if fileValue == nil {
		t.Fatal("gotArgs[0] = nil, want non-nil empty []byte")
	}
	if len(fileValue) != 0 {
		t.Fatalf("len(gotArgs[0]) = %d, want 0", len(fileValue))
	}

	if gotArgs[1] != nil {
		t.Fatalf("gotArgs[1] = %#v, want nil", gotArgs[1])
	}
}

func TestInsertBatchReturnsRowContextOnMismatch(t *testing.T) {
	client := &TargetClient{}

	err := client.InsertBatch(context.Background(), "users", RowBatch{
		Columns: []string{"id", "name"},
		Rows: [][]any{
			{int64(1)},
		},
	})
	if err == nil {
		t.Fatal("InsertBatch() error = nil, want error")
	}

	if !strings.Contains(err.Error(), `row 0`) {
		t.Fatalf("error = %q", err)
	}
}

func TestInsertBatchReturnsSafeExecContext(t *testing.T) {
	wantErr := errors.New("boom")
	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				return nil, wantErr
			},
		},
	}

	err := client.InsertBatch(context.Background(), "users", RowBatch{
		Columns: []string{"id", "password"},
		Rows: [][]any{
			{int64(1), "secret"},
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("InsertBatch() error = %v, want %v", err, wantErr)
	}

	if !strings.Contains(err.Error(), `rows 0-0`) {
		t.Fatalf("error = %q", err)
	}

	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked row values: %q", err)
	}
}

func TestInsertBatchPreservesExplicitZeroValues(t *testing.T) {
	var gotArgs []any

	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				gotArgs = append([]any(nil), args...)
				return fakeResult{}, nil
			},
		},
	}

	err := client.InsertBatch(context.Background(), "gate_users", RowBatch{
		Columns: []string{"id", "created_by"},
		Rows: [][]any{
			{int64(0), int64(0)},
		},
	})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}

	want := []any{int64(0), int64(0)}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args = %#v, want %#v", gotArgs, want)
	}
}

func TestPrepareSyncSessionSetsModeAndDisablesForeignKeys(t *testing.T) {
	var queries []string
	var argsList [][]any

	client := &TargetClient{
		session: fakeTargetSession{
			queryRow: func(ctx context.Context, query string, args ...any) targetScanner {
				return fakeTargetScanner{
					scan: func(dest ...any) error {
						*dest[0].(*string) = "STRICT_TRANS_TABLES"
						return nil
					},
				}
			},
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				queries = append(queries, query)
				argsList = append(argsList, append([]any(nil), args...))
				return fakeResult{}, nil
			},
		},
	}

	if err := client.PrepareSyncSession(context.Background()); err != nil {
		t.Fatalf("PrepareSyncSession() error = %v", err)
	}

	wantQueries := []string{
		"SET SESSION sql_mode = ?",
		"SET FOREIGN_KEY_CHECKS = 0",
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", queries, wantQueries)
	}
	if got := argsList[0][0]; got != "STRICT_TRANS_TABLES,NO_AUTO_VALUE_ON_ZERO" {
		t.Fatalf("sql_mode arg = %#v", got)
	}
}

func TestPrepareSyncSessionDoesNotDuplicateNoAutoValueOnZero(t *testing.T) {
	var modeArg any

	client := &TargetClient{
		session: fakeTargetSession{
			queryRow: func(ctx context.Context, query string, args ...any) targetScanner {
				return fakeTargetScanner{
					scan: func(dest ...any) error {
						*dest[0].(*string) = "STRICT_TRANS_TABLES,NO_AUTO_VALUE_ON_ZERO"
						return nil
					},
				}
			},
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				if query == "SET SESSION sql_mode = ?" {
					modeArg = args[0]
				}
				return fakeResult{}, nil
			},
		},
	}

	if err := client.PrepareSyncSession(context.Background()); err != nil {
		t.Fatalf("PrepareSyncSession() error = %v", err)
	}

	if modeArg != "STRICT_TRANS_TABLES,NO_AUTO_VALUE_ON_ZERO" {
		t.Fatalf("modeArg = %#v", modeArg)
	}
}

func TestCloseRestoresForeignKeysAndSQLMode(t *testing.T) {
	var queries []string
	var argsList [][]any
	sessionClosed := false
	dbClosed := false

	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				queries = append(queries, query)
				argsList = append(argsList, append([]any(nil), args...))
				return fakeResult{}, nil
			},
		},
		closeSes: func() error {
			sessionClosed = true
			return nil
		},
		closeDB: func() error {
			dbClosed = true
			return nil
		},
		originalSQLMode:          "STRICT_TRANS_TABLES",
		foreignKeyChecksDisabled: true,
		syncPrepared:             true,
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	wantQueries := []string{
		"SET FOREIGN_KEY_CHECKS = 1",
		"SET SESSION sql_mode = ?",
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", queries, wantQueries)
	}
	if got := argsList[1][0]; got != "STRICT_TRANS_TABLES" {
		t.Fatalf("restore sql_mode arg = %#v", got)
	}
	if !sessionClosed || !dbClosed {
		t.Fatalf("sessionClosed = %v, dbClosed = %v", sessionClosed, dbClosed)
	}
}

func TestCloseSyncSessionReleasesSchemaSession(t *testing.T) {
	var queries []string
	closed := false

	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				queries = append(queries, query)
				return fakeResult{}, nil
			},
		},
		closeSes: func() error {
			closed = true
			return nil
		},
		originalSQLMode:          "STRICT_TRANS_TABLES",
		foreignKeyChecksDisabled: true,
		syncPrepared:             true,
	}

	if err := client.CloseSyncSession(context.Background()); err != nil {
		t.Fatalf("CloseSyncSession() error = %v", err)
	}

	wantQueries := []string{
		"SET FOREIGN_KEY_CHECKS = 1",
		"SET SESSION sql_mode = ?",
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", queries, wantQueries)
	}
	if !closed {
		t.Fatal("session was not closed")
	}
	if client.session != nil || client.closeSes != nil || client.syncPrepared {
		t.Fatalf("client kept schema session state: %#v", client)
	}
}

func TestInsertBatchSplitsPacketTooLargeError(t *testing.T) {
	calls := 0
	var rowCounts []int
	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				calls++
				rowCounts = append(rowCounts, len(args))
				if calls == 1 {
					return nil, &mysql.MySQLError{Number: 1153, Message: "packet bigger than max_allowed_packet"}
				}
				return fakeResult{}, nil
			},
		},
	}

	err := client.InsertBatch(context.Background(), "documents", RowBatch{
		Columns: []string{"id"},
		Rows:    [][]any{{int64(1)}, {int64(2)}, {int64(3)}, {int64(4)}},
	})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}

	if !reflect.DeepEqual(rowCounts, []int{4, 2, 2}) {
		t.Fatalf("rowCounts = %#v", rowCounts)
	}
}

func TestInsertBatchDoesNotSplitNonPacketError(t *testing.T) {
	wantErr := errors.New("duplicate key")
	calls := 0
	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				calls++
				return nil, wantErr
			},
		},
	}

	err := client.InsertBatch(context.Background(), "users", RowBatch{
		Columns: []string{"id"},
		Rows:    [][]any{{int64(1)}, {int64(2)}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("InsertBatch() error = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestInsertBatchSinglePacketTooLargeReturnsClearError(t *testing.T) {
	client := &TargetClient{
		session: fakeTargetSession{
			exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				return nil, &mysql.MySQLError{Number: 1153, Message: "packet bigger than max_allowed_packet"}
			},
		},
	}

	err := client.InsertBatch(context.Background(), "documents", RowBatch{
		Columns: []string{"payload"},
		Rows:    [][]any{{strings.Repeat("x", 1024)}},
	})
	if err == nil {
		t.Fatal("InsertBatch() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "max_allowed_packet") || !strings.Contains(err.Error(), "documents") {
		t.Fatalf("error = %q", err)
	}
}

func TestNewSessionSetsUpAndRestoresEachWorkerSession(t *testing.T) {
	var queries []string
	var argsList [][]any
	closed := false
	client := &TargetClient{
		newSes: func(ctx context.Context) (targetSession, func() error, error) {
			return fakeTargetSession{
					queryRow: func(ctx context.Context, query string, args ...any) targetScanner {
						return fakeTargetScanner{
							scan: func(dest ...any) error {
								*dest[0].(*string) = "STRICT_TRANS_TABLES"
								return nil
							},
						}
					},
					exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
						queries = append(queries, query)
						argsList = append(argsList, append([]any(nil), args...))
						return fakeResult{}, nil
					},
				}, func() error {
					closed = true
					return nil
				}, nil
		},
	}

	session, err := client.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	wantQueries := []string{
		"SET SESSION sql_mode = ?",
		"SET FOREIGN_KEY_CHECKS = 0",
		"SET FOREIGN_KEY_CHECKS = 1",
		"SET SESSION sql_mode = ?",
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", queries, wantQueries)
	}
	if argsList[0][0] != "STRICT_TRANS_TABLES,NO_AUTO_VALUE_ON_ZERO" || argsList[3][0] != "STRICT_TRANS_TABLES" {
		t.Fatalf("argsList = %#v", argsList)
	}
	if !closed {
		t.Fatal("session was not closed")
	}
}

func TestConfigurePoolUsesWorkerLimit(t *testing.T) {
	db, err := sql.Open("mysql", "root@tcp(127.0.0.1:3306)/app")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	configurePool(db, 3)
	if db.Stats().MaxOpenConnections != 3 {
		t.Fatalf("MaxOpenConnections = %d, want 3", db.Stats().MaxOpenConnections)
	}
}

type fakeTargetSession struct {
	ping     func(context.Context) error
	exec     func(context.Context, string, ...any) (sql.Result, error)
	queryRow func(context.Context, string, ...any) targetScanner
	beginTx  func(context.Context, *sql.TxOptions) (targetTransaction, error)
}

func (p fakeTargetSession) PingContext(ctx context.Context) error {
	if p.ping != nil {
		return p.ping(ctx)
	}
	return nil
}

func (p fakeTargetSession) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if p.exec != nil {
		return p.exec(ctx, query, args...)
	}
	return fakeResult{}, nil
}

func (p fakeTargetSession) QueryRowContext(ctx context.Context, query string, args ...any) targetScanner {
	if p.queryRow != nil {
		return p.queryRow(ctx, query, args...)
	}
	return fakeTargetScanner{}
}

func (p fakeTargetSession) BeginTx(ctx context.Context, opts *sql.TxOptions) (targetTransaction, error) {
	if p.beginTx != nil {
		return p.beginTx(ctx, opts)
	}
	return &fakeTargetTx{}, nil
}

type fakeTargetTx struct {
	exec     func(context.Context, string, ...any) (sql.Result, error)
	commit   func() error
	rollback func() error
}

func (tx *fakeTargetTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if tx.exec != nil {
		return tx.exec(ctx, query, args...)
	}
	return fakeResult{}, nil
}

func (tx *fakeTargetTx) Commit() error {
	if tx.commit != nil {
		return tx.commit()
	}
	return nil
}

func (tx *fakeTargetTx) Rollback() error {
	if tx.rollback != nil {
		return tx.rollback()
	}
	return nil
}

type fakeTargetScanner struct {
	scan func(dest ...any) error
}

func (s fakeTargetScanner) Scan(dest ...any) error {
	if s.scan != nil {
		return s.scan(dest...)
	}
	return nil
}

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (fakeResult) RowsAffected() (int64, error) {
	return 0, nil
}
