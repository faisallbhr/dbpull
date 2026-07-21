package sync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"dbpull/internal/db"
)

type DataSyncer struct {
	source   dataSource
	target   dataTarget
	progress func(DataProgress)
}

type DataProgressKind string

const (
	DataProgressTableStart    DataProgressKind = "table_start"
	DataProgressBatchAdjusted DataProgressKind = "batch_adjusted"
	DataProgressTableProgress DataProgressKind = "table_progress"
	DataProgressTableComplete DataProgressKind = "table_complete"
	DataProgressTableFailed   DataProgressKind = "table_failed"
	DataProgressDataExcluded  DataProgressKind = "data_excluded"
)

type DataProgress struct {
	Kind                DataProgressKind
	Table               string
	TableIndex          int
	TotalTables         int
	TableRows           int64
	TotalRows           int64
	BatchNumber         int
	ConfiguredBatchSize int
	EffectiveBatchSize  int
	ColumnCount         int
	Err                 error
}

type dataSource interface {
	StreamRows(
		ctx context.Context,
		table string,
		batchSize int,
		maxBatchBytes int,
		notice db.BatchSizeNotice,
		handle db.RowBatchHandler,
	) error
}

type dataTarget interface {
	NewSession(ctx context.Context) (db.DataSession, error)
}

func NewDataSyncer(source dataSource, target dataTarget, progress func(DataProgress)) *DataSyncer {
	return &DataSyncer{
		source:   source,
		target:   target,
		progress: progress,
	}
}

func (s *DataSyncer) Sync(ctx context.Context, plan SyncPlan, batchSize int, maxBatchBytes int, transactionBatches int, workers int) error {
	if batchSize <= 0 {
		return fmt.Errorf("sync data: batch size must be greater than 0")
	}
	if maxBatchBytes <= 0 {
		return fmt.Errorf("sync data: max batch bytes must be greater than 0")
	}
	if transactionBatches <= 0 {
		return fmt.Errorf("sync data: transaction batches must be greater than 0")
	}
	if workers <= 0 {
		return fmt.Errorf("sync data: workers must be greater than 0")
	}

	if workers == 1 {
		return s.syncSequential(ctx, plan, batchSize, maxBatchBytes, transactionBatches)
	}

	return s.syncParallel(ctx, plan, batchSize, maxBatchBytes, transactionBatches, workers)
}

func (s *DataSyncer) syncSequential(ctx context.Context, plan SyncPlan, batchSize int, maxBatchBytes int, transactionBatches int) error {
	var totalRows int64
	for index, table := range plan.Tables {
		if _, err := s.syncTable(ctx, table, index, len(plan.Tables), batchSize, maxBatchBytes, transactionBatches, &totalRows, nil); err != nil {
			return err
		}
	}

	return nil
}

func (s *DataSyncer) syncParallel(ctx context.Context, plan SyncPlan, batchSize int, maxBatchBytes int, transactionBatches int, workers int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type job struct {
		table PlanTable
		index int
	}

	jobs := make(chan job)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var totalRows int64

	workerCount := workers
	if workerCount > len(plan.Tables) {
		workerCount = len(plan.Tables)
	}

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				_, err := s.syncTable(ctx, job.table, job.index, len(plan.Tables), batchSize, maxBatchBytes, transactionBatches, &totalRows, &mu)
				if err != nil {
					select {
					case errs <- err:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}

sendJobs:
	for index, table := range plan.Tables {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- job{table: table, index: index}:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errs:
		return err
	default:
		return ctx.Err()
	}
}

func (s *DataSyncer) syncTable(
	ctx context.Context,
	table PlanTable,
	index int,
	totalTables int,
	batchSize int,
	maxBatchBytes int,
	transactionBatches int,
	totalRows *int64,
	mu *sync.Mutex,
) (tableRows int64, retErr error) {
	progress := func(update DataProgress) {
		if s.progress == nil {
			return
		}
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		update.TotalRows = *totalRows
		s.progress(update)
	}

	addRows := func(rows int64) {
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		*totalRows += rows
	}

	if table.DataExcluded {
		progress(DataProgress{
			Kind:        DataProgressDataExcluded,
			Table:       table.Name,
			TableIndex:  index + 1,
			TotalTables: totalTables,
		})
		return 0, nil
	}

	progress(DataProgress{
		Kind:        DataProgressTableStart,
		Table:       table.Name,
		TableIndex:  index + 1,
		TotalTables: totalTables,
	})

	session, err := s.target.NewSession(ctx)
	if err != nil {
		return 0, fmt.Errorf("sync data for %q: %w", table.Name, err)
	}
	defer func() {
		if err := session.Close(context.Background()); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("restore target session for %q: %w", table.Name, err))
		}
	}()

	batchNumber := 0
	batchesInTx := 0
	pendingRows := int64(0)
	var tx db.DataTx

	rollback := func() error {
		if tx != nil {
			err := tx.Rollback()
			tx = nil
			batchesInTx = 0
			pendingRows = 0
			if err != nil {
				return fmt.Errorf("rollback target transaction for %q after batch %d: %w", table.Name, batchNumber, err)
			}
		}
		return nil
	}
	commit := func() error {
		if tx == nil {
			return nil
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit target transaction for %q after batch %d: %w", table.Name, batchNumber, err)
		}
		tx = nil
		batchesInTx = 0
		tableRows += pendingRows
		addRows(pendingRows)
		pendingRows = 0
		progress(DataProgress{
			Kind:        DataProgressTableProgress,
			Table:       table.Name,
			TableIndex:  index + 1,
			TotalTables: totalTables,
			TableRows:   tableRows,
			BatchNumber: batchNumber,
		})
		return nil
	}

	err = s.source.StreamRows(
		ctx,
		table.Name,
		batchSize,
		maxBatchBytes,
		func(adjustment db.BatchSizeAdjustment) {
			progress(DataProgress{
				Kind:                DataProgressBatchAdjusted,
				Table:               table.Name,
				TableIndex:          index + 1,
				TotalTables:         totalTables,
				ConfiguredBatchSize: adjustment.ConfiguredSize,
				EffectiveBatchSize:  adjustment.EffectiveSize,
				ColumnCount:         adjustment.ColumnCount,
			})
		},
		func(batch db.RowBatch) error {
			if err := ctx.Err(); err != nil {
				return errors.Join(err, rollback())
			}
			if tx == nil {
				var err error
				tx, err = session.BeginTx(ctx)
				if err != nil {
					return err
				}
			}

			batchNumber++
			if err := tx.InsertBatch(ctx, table.Name, batch); err != nil {
				rollbackErr := rollback()
				progress(DataProgress{
					Kind:        DataProgressTableFailed,
					Table:       table.Name,
					TableIndex:  index + 1,
					TotalTables: totalTables,
					TableRows:   tableRows,
					BatchNumber: batchNumber,
					Err:         err,
				})
				return errors.Join(err, rollbackErr)
			}

			batchesInTx++
			pendingRows += int64(len(batch.Rows))

			if batchesInTx == transactionBatches {
				if err := commit(); err != nil {
					return errors.Join(err, rollback())
				}
			}

			return nil
		},
	)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("sync data for %q: %w", table.Name, err), rollback())
	}
	if err := commit(); err != nil {
		return 0, errors.Join(fmt.Errorf("sync data for %q: %w", table.Name, err), rollback())
	}

	progress(DataProgress{
		Kind:        DataProgressTableComplete,
		Table:       table.Name,
		TableIndex:  index + 1,
		TotalTables: totalTables,
		TableRows:   tableRows,
		BatchNumber: batchNumber,
	})

	return tableRows, nil
}
