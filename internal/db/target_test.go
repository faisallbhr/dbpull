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
		db: fakeTargetDB{
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
		db: fakeTargetDB{
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
		db: fakeTargetDB{
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
		db: fakeTargetDB{
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
		db: fakeTargetDB{
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

type fakeTargetDB struct {
	ping func(context.Context) error
	exec func(context.Context, string, ...any) (sql.Result, error)
}

func (p fakeTargetDB) PingContext(ctx context.Context) error {
	if p.ping != nil {
		return p.ping(ctx)
	}
	return nil
}

func (p fakeTargetDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if p.exec != nil {
		return p.exec(ctx, query, args...)
	}
	return fakeResult{}, nil
}

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (fakeResult) RowsAffected() (int64, error) {
	return 0, nil
}
