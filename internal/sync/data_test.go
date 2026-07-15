package sync

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dbpull/internal/db"
)

func TestDataSyncerSync(t *testing.T) {
	var inserted []string
	var progress []DataProgress

	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(
				ctx context.Context,
				table string,
				batchSize int,
				notice db.BatchSizeNotice,
				handle db.RowBatchHandler,
			) error {
				switch table {
				case "users":
					if err := handle(db.RowBatch{
						Columns: []string{"id"},
						Rows:    [][]any{{int64(1)}, {int64(2)}},
					}); err != nil {
						return err
					}
				case "products":
					if err := handle(db.RowBatch{
						Columns: []string{"id"},
						Rows:    [][]any{{int64(10)}},
					}); err != nil {
						return err
					}
				}
				return nil
			},
		},
		fakeDataTarget{
			insertBatch: func(ctx context.Context, table string, batch db.RowBatch) error {
				inserted = append(inserted, table)
				return nil
			},
		},
		func(update DataProgress) {
			progress = append(progress, update)
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{
			{Name: "users"},
			{Name: "products"},
		},
	}, 2)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !reflect.DeepEqual(inserted, []string{"users", "products"}) {
		t.Fatalf("inserted = %#v", inserted)
	}

	wantProgress := []DataProgress{
		{Kind: DataProgressTableStart, Table: "users", TableIndex: 1, TotalTables: 2},
		{Kind: DataProgressTableProgress, Table: "users", TableIndex: 1, TableRows: 2, TotalRows: 2, TotalTables: 2, BatchNumber: 1},
		{Kind: DataProgressTableComplete, Table: "users", TableIndex: 1, TableRows: 2, TotalRows: 2, TotalTables: 2, BatchNumber: 1},
		{Kind: DataProgressTableStart, Table: "products", TableIndex: 2, TotalTables: 2, TotalRows: 2},
		{Kind: DataProgressTableProgress, Table: "products", TableIndex: 2, TableRows: 1, TotalRows: 3, TotalTables: 2, BatchNumber: 1},
		{Kind: DataProgressTableComplete, Table: "products", TableIndex: 2, TableRows: 1, TotalRows: 3, TotalTables: 2, BatchNumber: 1},
	}
	if len(progress) != len(wantProgress) {
		t.Fatalf("len(progress) = %d, want %d", len(progress), len(wantProgress))
	}
	for i := range progress {
		if progress[i].Kind != wantProgress[i].Kind ||
			progress[i].Table != wantProgress[i].Table ||
			progress[i].TableIndex != wantProgress[i].TableIndex ||
			progress[i].TotalTables != wantProgress[i].TotalTables ||
			progress[i].TableRows != wantProgress[i].TableRows ||
			progress[i].TotalRows != wantProgress[i].TotalRows ||
			progress[i].BatchNumber != wantProgress[i].BatchNumber {
			t.Fatalf("progress[%d] = %#v, want %#v", i, progress[i], wantProgress[i])
		}
	}
}

func TestDataSyncerStopsOnFirstError(t *testing.T) {
	wantErr := errors.New("boom")
	calls := 0

	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(
				ctx context.Context,
				table string,
				batchSize int,
				notice db.BatchSizeNotice,
				handle db.RowBatchHandler,
			) error {
				calls++
				return handle(db.RowBatch{
					Columns: []string{"id"},
					Rows:    [][]any{{int64(1)}},
				})
			},
		},
		fakeDataTarget{
			insertBatch: func(ctx context.Context, table string, batch db.RowBatch) error {
				return wantErr
			},
		},
		nil,
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{
			{Name: "users"},
			{Name: "products"},
		},
	}, 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Sync() error = %v, want %v", err, wantErr)
	}

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDataSyncerReportsBatchSizeReduction(t *testing.T) {
	var progress []DataProgress

	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(
				ctx context.Context,
				table string,
				batchSize int,
				notice db.BatchSizeNotice,
				handle db.RowBatchHandler,
			) error {
				notice(db.BatchSizeAdjustment{
					Table:          table,
					ConfiguredSize: 1000,
					EffectiveSize:  869,
					ColumnCount:    69,
				})
				return handle(db.RowBatch{
					Columns: []string{"id"},
					Rows:    [][]any{{int64(1)}},
				})
			},
		},
		fakeDataTarget{},
		func(update DataProgress) {
			progress = append(progress, update)
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{{Name: "omni_sales_orders"}},
	}, 1000)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if len(progress) != 4 {
		t.Fatalf("len(progress) = %d, want 4", len(progress))
	}

	if progress[1].Kind != DataProgressBatchAdjusted {
		t.Fatalf("progress[1].Kind = %q", progress[1].Kind)
	}
	if progress[1].ConfiguredBatchSize != 1000 || progress[1].EffectiveBatchSize != 869 || progress[1].ColumnCount != 69 {
		t.Fatalf("progress[1] = %#v", progress[1])
	}
}

func TestDataSyncerSkipsExcludedData(t *testing.T) {
	called := false
	var progress []DataProgress

	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(
				ctx context.Context,
				table string,
				batchSize int,
				notice db.BatchSizeNotice,
				handle db.RowBatchHandler,
			) error {
				called = true
				return nil
			},
		},
		fakeDataTarget{},
		func(update DataProgress) {
			progress = append(progress, update)
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{{Name: "audits", DataExcluded: true}},
	}, 1000)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if called {
		t.Fatal("StreamRows() called for data-excluded table")
	}

	if len(progress) != 1 || progress[0].Kind != DataProgressDataExcluded || progress[0].Table != "audits" {
		t.Fatalf("progress = %#v", progress)
	}
}

type fakeDataSource struct {
	streamRows func(context.Context, string, int, db.BatchSizeNotice, db.RowBatchHandler) error
}

func (f fakeDataSource) StreamRows(
	ctx context.Context,
	table string,
	batchSize int,
	notice db.BatchSizeNotice,
	handle db.RowBatchHandler,
) error {
	if f.streamRows != nil {
		return f.streamRows(ctx, table, batchSize, notice, handle)
	}
	return nil
}

type fakeDataTarget struct {
	insertBatch func(context.Context, string, db.RowBatch) error
}

func (f fakeDataTarget) InsertBatch(ctx context.Context, table string, batch db.RowBatch) error {
	if f.insertBatch != nil {
		return f.insertBatch(ctx, table, batch)
	}
	return nil
}
