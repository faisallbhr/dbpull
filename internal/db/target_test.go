package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"dbpull/internal/config"
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

type fakeTargetSession struct {
	ping     func(context.Context) error
	exec     func(context.Context, string, ...any) (sql.Result, error)
	queryRow func(context.Context, string, ...any) targetScanner
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
