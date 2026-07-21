package sync

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/faisallbhr/dbpull/internal/db"
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
				maxBatchBytes int,
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
		&fakeDataTarget{
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
	}, 2, 1024, 20, 1)
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
				maxBatchBytes int,
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
		&fakeDataTarget{
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
	}, 1, 1024, 20, 1)
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
				maxBatchBytes int,
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
		&fakeDataTarget{},
		func(update DataProgress) {
			progress = append(progress, update)
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{{Name: "omni_sales_orders"}},
	}, 1000, 1024, 20, 1)
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
				maxBatchBytes int,
				notice db.BatchSizeNotice,
				handle db.RowBatchHandler,
			) error {
				called = true
				return nil
			},
		},
		&fakeDataTarget{},
		func(update DataProgress) {
			progress = append(progress, update)
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{{Name: "audits", DataExcluded: true}},
	}, 1000, 1024, 20, 1)
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

func TestDataSyncerCommitsEveryNBatchAndRemainder(t *testing.T) {
	target := &fakeDataTarget{}
	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(ctx context.Context, table string, batchSize int, maxBatchBytes int, notice db.BatchSizeNotice, handle db.RowBatchHandler) error {
				for i := 0; i < 5; i++ {
					if err := handle(db.RowBatch{Columns: []string{"id"}, Rows: [][]any{{int64(i)}}}); err != nil {
						return err
					}
				}
				return nil
			},
		},
		target,
		nil,
	)

	err := syncer.Sync(context.Background(), SyncPlan{Tables: []PlanTable{{Name: "users"}}}, 1, 1024, 2, 1)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if target.commits != 3 {
		t.Fatalf("commits = %d, want 3", target.commits)
	}
	if target.rollbacks != 0 {
		t.Fatalf("rollbacks = %d, want 0", target.rollbacks)
	}
}

func TestDataSyncerReportsOnlyCommittedRows(t *testing.T) {
	var progress []DataProgress
	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(ctx context.Context, table string, batchSize int, maxBatchBytes int, notice db.BatchSizeNotice, handle db.RowBatchHandler) error {
				for i := 0; i < 3; i++ {
					if err := handle(db.RowBatch{Columns: []string{"id"}, Rows: [][]any{{int64(i)}}}); err != nil {
						return err
					}
				}
				return nil
			},
		},
		&fakeDataTarget{},
		func(update DataProgress) {
			if update.Kind == DataProgressTableProgress {
				progress = append(progress, update)
			}
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{Tables: []PlanTable{{Name: "users"}}}, 1, 1024, 2, 1)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	wantRows := []int64{2, 3}
	if len(progress) != len(wantRows) {
		t.Fatalf("len(progress) = %d, want %d: %#v", len(progress), len(wantRows), progress)
	}
	for i, want := range wantRows {
		if progress[i].TableRows != want || progress[i].TotalRows != want {
			t.Fatalf("progress[%d] = %#v, want committed rows %d", i, progress[i], want)
		}
	}
}

func TestDataSyncerRollsBackOnInsertError(t *testing.T) {
	wantErr := errors.New("insert failed")
	target := &fakeDataTarget{
		insertBatch: func(ctx context.Context, table string, batch db.RowBatch) error {
			return wantErr
		},
	}
	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(ctx context.Context, table string, batchSize int, maxBatchBytes int, notice db.BatchSizeNotice, handle db.RowBatchHandler) error {
				return handle(db.RowBatch{Columns: []string{"id"}, Rows: [][]any{{int64(1)}}})
			},
		},
		target,
		nil,
	)

	err := syncer.Sync(context.Background(), SyncPlan{Tables: []PlanTable{{Name: "users"}}}, 1, 1024, 20, 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Sync() error = %v, want %v", err, wantErr)
	}
	if target.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want 1", target.rollbacks)
	}
}

func TestDataSyncerRollsBackOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	target := &fakeDataTarget{}
	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(ctx context.Context, table string, batchSize int, maxBatchBytes int, notice db.BatchSizeNotice, handle db.RowBatchHandler) error {
				if err := handle(db.RowBatch{Columns: []string{"id"}, Rows: [][]any{{int64(1)}}}); err != nil {
					return err
				}
				cancel()
				return ctx.Err()
			},
		},
		target,
		nil,
	)

	err := syncer.Sync(ctx, SyncPlan{Tables: []PlanTable{{Name: "users"}}}, 1, 1024, 20, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sync() error = %v, want context.Canceled", err)
	}
	if target.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want 1", target.rollbacks)
	}
}

func TestDataSyncerProcessesTablesWithLimitedWorkers(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	seen := map[string]bool{}

	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(ctx context.Context, table string, batchSize int, maxBatchBytes int, notice db.BatchSizeNotice, handle db.RowBatchHandler) error {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				seen[table] = true
				mu.Unlock()

				time.Sleep(10 * time.Millisecond)
				err := handle(db.RowBatch{Columns: []string{"id"}, Rows: [][]any{{int64(1)}}})

				mu.Lock()
				active--
				mu.Unlock()
				return err
			},
		},
		&fakeDataTarget{},
		nil,
	)

	err := syncer.Sync(context.Background(), SyncPlan{Tables: []PlanTable{{Name: "a"}, {Name: "b"}, {Name: "c"}}}, 1, 1024, 20, 2)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if maxActive != 2 {
		t.Fatalf("maxActive = %d, want 2", maxActive)
	}
	if len(seen) != 3 {
		t.Fatalf("seen = %#v", seen)
	}
}

func TestDataSyncerCancelsWorkersOnFirstError(t *testing.T) {
	wantErr := errors.New("boom")
	started := make(chan string, 2)
	release := make(chan struct{})

	syncer := NewDataSyncer(
		fakeDataSource{
			streamRows: func(ctx context.Context, table string, batchSize int, maxBatchBytes int, notice db.BatchSizeNotice, handle db.RowBatchHandler) error {
				started <- table
				if table == "bad" {
					return wantErr
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
					return nil
				}
			},
		},
		&fakeDataTarget{},
		nil,
	)
	defer close(release)

	err := syncer.Sync(context.Background(), SyncPlan{Tables: []PlanTable{{Name: "slow"}, {Name: "bad"}, {Name: "never"}}}, 1, 1024, 20, 2)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Sync() error = %v, want %v", err, wantErr)
	}
	if len(started) != 2 {
		t.Fatalf("started workers = %d, want 2", len(started))
	}
}

type fakeDataSource struct {
	streamRows func(context.Context, string, int, int, db.BatchSizeNotice, db.RowBatchHandler) error
}

func (f fakeDataSource) StreamRows(
	ctx context.Context,
	table string,
	batchSize int,
	maxBatchBytes int,
	notice db.BatchSizeNotice,
	handle db.RowBatchHandler,
) error {
	if f.streamRows != nil {
		return f.streamRows(ctx, table, batchSize, maxBatchBytes, notice, handle)
	}
	return nil
}

type fakeDataTarget struct {
	insertBatch func(context.Context, string, db.RowBatch) error
	commits     int
	rollbacks   int
	mu          sync.Mutex
}

func (f *fakeDataTarget) NewSession(ctx context.Context) (db.DataSession, error) {
	return &fakeDataSession{target: f}, nil
}

type fakeDataSession struct {
	target *fakeDataTarget
}

func (s *fakeDataSession) BeginTx(ctx context.Context) (db.DataTx, error) {
	return &fakeDataTx{target: s.target}, nil
}

func (s *fakeDataSession) Close(ctx context.Context) error {
	return nil
}

type fakeDataTx struct {
	target *fakeDataTarget
}

func (tx *fakeDataTx) InsertBatch(ctx context.Context, table string, batch db.RowBatch) error {
	if tx.target.insertBatch != nil {
		return tx.target.insertBatch(ctx, table, batch)
	}
	return nil
}

func (tx *fakeDataTx) Commit() error {
	tx.target.mu.Lock()
	defer tx.target.mu.Unlock()
	tx.target.commits++
	return nil
}

func (tx *fakeDataTx) Rollback() error {
	tx.target.mu.Lock()
	defer tx.target.mu.Unlock()
	tx.target.rollbacks++
	return nil
}
