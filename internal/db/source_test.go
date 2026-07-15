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

func TestBuildSourceDSN(t *testing.T) {
	dsn, err := buildSourceDSN(config.SourceConfig{
		Database: "olshoperp",
		Username: "remote_db",
		Password: "secret",
	}, "127.0.0.1:4306")
	if err != nil {
		t.Fatalf("buildSourceDSN() error = %v", err)
	}

	for _, want := range []string{
		"remote_db:secret@tcp(127.0.0.1:4306)/olshoperp",
		"parseTime=true",
	} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn = %q, want substring %q", dsn, want)
		}
	}
}

func TestBuildSourceDSNReturnsErrorForInvalidTunnelAddress(t *testing.T) {
	_, err := buildSourceDSN(config.SourceConfig{}, "invalid")
	if err == nil {
		t.Fatal("buildSourceDSN() error = nil, want error")
	}
}

func TestPing(t *testing.T) {
	client := &SourceClient{
		db: fakeQueryer{
			ping: func(context.Context) error { return nil },
		},
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestListTables(t *testing.T) {
	client := &SourceClient{
		db: fakeQueryer{
			query: func(ctx context.Context, query string, args ...any) (rowSet, error) {
				return &fakeRows{
					columns: []string{"table_name"},
					rows: [][]any{
						{"products"},
						{"users"},
					},
				}, nil
			},
		},
		database: "olshoperp",
	}

	tables, err := client.ListTables(context.Background())
	if err != nil {
		t.Fatalf("ListTables() error = %v", err)
	}

	if len(tables) != 2 || tables[0].Name != "products" || tables[1].Name != "users" {
		t.Fatalf("ListTables() = %#v", tables)
	}
}

func TestShowCreateTable(t *testing.T) {
	client := &SourceClient{
		db: fakeQueryer{
			queryRow: func(ctx context.Context, query string, args ...any) scanner {
				return fakeScanner{
					scan: func(dest ...any) error {
						*dest[0].(*string) = "users"
						*dest[1].(*string) = "CREATE TABLE `users` (`id` bigint)"
						return nil
					},
				}
			},
		},
	}

	createSQL, err := client.ShowCreateTable(context.Background(), "users")
	if err != nil {
		t.Fatalf("ShowCreateTable() error = %v", err)
	}

	if createSQL != "CREATE TABLE `users` (`id` bigint)" {
		t.Fatalf("ShowCreateTable() = %q", createSQL)
	}
}

func TestStreamRowsBatchesRowsAndClonesBytes(t *testing.T) {
	sourceBytes := []byte("alice")
	client := &SourceClient{
		db: fakeQueryer{
			query: func(ctx context.Context, query string, args ...any) (rowSet, error) {
				return &fakeRows{
					columns: []string{"id", "name"},
					rows: [][]any{
						{int64(1), sourceBytes},
						{int64(2), []byte("bob")},
						{int64(3), nil},
					},
				}, nil
			},
		},
	}

	var batches []RowBatch
	err := client.StreamRows(context.Background(), "users", 2, nil, func(batch RowBatch) error {
		copied := RowBatch{
			Columns: append([]string(nil), batch.Columns...),
			Rows:    make([][]any, len(batch.Rows)),
		}
		for i, row := range batch.Rows {
			copied.Rows[i] = append([]any(nil), row...)
		}
		batches = append(batches, copied)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamRows() error = %v", err)
	}

	sourceBytes[0] = 'x'

	if len(batches) != 2 {
		t.Fatalf("len(batches) = %d, want 2", len(batches))
	}

	got := string(batches[0].Rows[0][1].([]byte))
	if got != "alice" {
		t.Fatalf("first streamed name = %q, want %q", got, "alice")
	}
}

func TestScanRowPreservesValueDistinctions(t *testing.T) {
	rows := &fakeRows{
		columns: []string{
			"null_string",
			"empty_string",
			"non_empty_string",
			"empty_blob",
			"non_empty_blob",
			"zero_integer",
			"zero_decimal",
			"false_tinyint",
		},
		rows: [][]any{
			{
				nil,
				"",
				"hello",
				[]byte{},
				[]byte{0x01, 0x02},
				int64(0),
				[]byte("0.00"),
				int64(0),
			},
		},
	}

	if !rows.Next() {
		t.Fatal("rows.Next() = false, want true")
	}

	got, err := scanRow(rows, len(rows.columns))
	if err != nil {
		t.Fatalf("scanRow() error = %v", err)
	}

	if got[0] != nil {
		t.Fatalf("got[0] = %#v, want nil", got[0])
	}

	if got[1] != "" {
		t.Fatalf("got[1] = %#v, want empty string", got[1])
	}

	if got[2] != "hello" {
		t.Fatalf("got[2] = %#v, want %q", got[2], "hello")
	}

	emptyBlob, ok := got[3].([]byte)
	if !ok {
		t.Fatalf("got[3] type = %T, want []byte", got[3])
	}
	if emptyBlob == nil {
		t.Fatal("got[3] = nil, want non-nil empty []byte")
	}
	if len(emptyBlob) != 0 {
		t.Fatalf("len(got[3]) = %d, want 0", len(emptyBlob))
	}

	nonEmptyBlob, ok := got[4].([]byte)
	if !ok {
		t.Fatalf("got[4] type = %T, want []byte", got[4])
	}
	if !reflect.DeepEqual(nonEmptyBlob, []byte{0x01, 0x02}) {
		t.Fatalf("got[4] = %#v", nonEmptyBlob)
	}

	if got[5] != int64(0) {
		t.Fatalf("got[5] = %#v, want int64(0)", got[5])
	}

	zeroDecimal, ok := got[6].([]byte)
	if !ok {
		t.Fatalf("got[6] type = %T, want []byte", got[6])
	}
	if string(zeroDecimal) != "0.00" {
		t.Fatalf("got[6] = %#v, want %q", zeroDecimal, "0.00")
	}

	if got[7] != int64(0) {
		t.Fatalf("got[7] = %#v, want int64(0)", got[7])
	}
}

func TestStreamRowsReturnsHandlerError(t *testing.T) {
	wantErr := errors.New("stop")
	client := &SourceClient{
		db: fakeQueryer{
			query: func(ctx context.Context, query string, args ...any) (rowSet, error) {
				return &fakeRows{
					columns: []string{"id"},
					rows:    [][]any{{int64(1)}},
				}, nil
			},
		},
	}

	err := client.StreamRows(context.Background(), "users", 1, nil, func(batch RowBatch) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("StreamRows() error = %v, want %v", err, wantErr)
	}
}

func TestCalculateEffectiveBatchSize(t *testing.T) {
	tests := []struct {
		name       string
		configured int
		columns    int
		want       int
	}{
		{name: "narrow table", configured: 1000, columns: 10, want: 1000},
		{name: "wide table", configured: 1000, columns: 69, want: 869},
		{name: "configured below limit", configured: 100, columns: 69, want: 100},
		{name: "configured above limit", configured: 1000, columns: 80, want: 750},
		{name: "one column table", configured: 1000, columns: 1, want: 1000},
		{name: "extremely wide table", configured: 1000, columns: 70000, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateEffectiveBatchSize(tt.configured, tt.columns)
			if got != tt.want {
				t.Fatalf("calculateEffectiveBatchSize(%d, %d) = %d, want %d", tt.configured, tt.columns, got, tt.want)
			}
		})
	}
}

func TestStreamRowsReportsBatchSizeReduction(t *testing.T) {
	columns := make([]string, 69)
	row := make([]any, len(columns))
	for i := range columns {
		columns[i] = "col"
		row[i] = int64(i)
	}

	client := &SourceClient{
		db: fakeQueryer{
			query: func(ctx context.Context, query string, args ...any) (rowSet, error) {
				return &fakeRows{
					columns: columns,
					rows:    [][]any{row},
				}, nil
			},
		},
	}

	var adjustment BatchSizeAdjustment
	err := client.StreamRows(
		context.Background(),
		"omni_sales_orders",
		1000,
		func(got BatchSizeAdjustment) {
			adjustment = got
		},
		func(batch RowBatch) error { return nil },
	)
	if err != nil {
		t.Fatalf("StreamRows() error = %v", err)
	}

	want := BatchSizeAdjustment{
		Table:          "omni_sales_orders",
		ConfiguredSize: 1000,
		EffectiveSize:  869,
		ColumnCount:    69,
	}
	if !reflect.DeepEqual(adjustment, want) {
		t.Fatalf("adjustment = %#v, want %#v", adjustment, want)
	}
}

func TestClose(t *testing.T) {
	closed := false
	client := &SourceClient{
		close: func() error {
			closed = true
			return nil
		},
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !closed {
		t.Fatal("Close() did not call close function")
	}
}

type fakeQueryer struct {
	ping     func(context.Context) error
	query    func(context.Context, string, ...any) (rowSet, error)
	queryRow func(context.Context, string, ...any) scanner
}

func (q fakeQueryer) PingContext(ctx context.Context) error {
	if q.ping != nil {
		return q.ping(ctx)
	}
	return nil
}

func (q fakeQueryer) QueryContext(ctx context.Context, query string, args ...any) (rowSet, error) {
	if q.query != nil {
		return q.query(ctx, query, args...)
	}
	return nil, errors.New("query not implemented")
}

func (q fakeQueryer) QueryRowContext(ctx context.Context, query string, args ...any) scanner {
	if q.queryRow != nil {
		return q.queryRow(ctx, query, args...)
	}
	return fakeScanner{scan: func(dest ...any) error { return sql.ErrNoRows }}
}

type fakeScanner struct {
	scan func(dest ...any) error
}

func (s fakeScanner) Scan(dest ...any) error {
	return s.scan(dest...)
}

type fakeRows struct {
	columns []string
	rows    [][]any
	index   int
	err     error
}

func (r *fakeRows) Columns() ([]string, error) {
	return append([]string(nil), r.columns...), nil
}

func (r *fakeRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return sql.ErrNoRows
	}

	row := r.rows[r.index]
	for i := range dest {
		switch ptr := dest[i].(type) {
		case *any:
			*ptr = row[i]
		case *string:
			value, _ := row[i].(string)
			*ptr = value
		default:
			return errors.New("unsupported scan destination")
		}
	}

	r.index++
	return nil
}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) Close() error {
	return nil
}
