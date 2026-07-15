package sync

import (
	"context"
	"fmt"

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
		notice db.BatchSizeNotice,
		handle db.RowBatchHandler,
	) error
}

type dataTarget interface {
	InsertBatch(ctx context.Context, table string, batch db.RowBatch) error
}

func NewDataSyncer(source dataSource, target dataTarget, progress func(DataProgress)) *DataSyncer {
	return &DataSyncer{
		source:   source,
		target:   target,
		progress: progress,
	}
}

func (s *DataSyncer) Sync(ctx context.Context, plan SyncPlan, batchSize int) error {
	if batchSize <= 0 {
		return fmt.Errorf("sync data: batch size must be greater than 0")
	}

	var totalRows int64
	for index, table := range plan.Tables {
		if table.DataExcluded {
			if s.progress != nil {
				s.progress(DataProgress{
					Kind:        DataProgressDataExcluded,
					Table:       table.Name,
					TableIndex:  index + 1,
					TotalTables: len(plan.Tables),
					TotalRows:   totalRows,
				})
			}
			continue
		}

		if s.progress != nil {
			s.progress(DataProgress{
				Kind:        DataProgressTableStart,
				Table:       table.Name,
				TableIndex:  index + 1,
				TotalTables: len(plan.Tables),
				TotalRows:   totalRows,
			})
		}

		var tableRows int64
		batchNumber := 0

		err := s.source.StreamRows(
			ctx,
			table.Name,
			batchSize,
			func(adjustment db.BatchSizeAdjustment) {
				if s.progress == nil {
					return
				}

				s.progress(DataProgress{
					Kind:                DataProgressBatchAdjusted,
					Table:               table.Name,
					TableIndex:          index + 1,
					TotalTables:         len(plan.Tables),
					TotalRows:           totalRows,
					ConfiguredBatchSize: adjustment.ConfiguredSize,
					EffectiveBatchSize:  adjustment.EffectiveSize,
					ColumnCount:         adjustment.ColumnCount,
				})
			},
			func(batch db.RowBatch) error {
				batchNumber++

				if err := s.target.InsertBatch(ctx, table.Name, batch); err != nil {
					if s.progress != nil {
						s.progress(DataProgress{
							Kind:        DataProgressTableFailed,
							Table:       table.Name,
							TableIndex:  index + 1,
							TotalTables: len(plan.Tables),
							TableRows:   tableRows,
							TotalRows:   totalRows,
							BatchNumber: batchNumber,
							Err:         err,
						})
					}
					return err
				}

				batchRows := int64(len(batch.Rows))
				tableRows += batchRows
				totalRows += batchRows

				if s.progress != nil {
					s.progress(DataProgress{
						Kind:        DataProgressTableProgress,
						Table:       table.Name,
						TableIndex:  index + 1,
						TotalTables: len(plan.Tables),
						TableRows:   tableRows,
						TotalRows:   totalRows,
						BatchNumber: batchNumber,
					})
				}

				return nil
			},
		)
		if err != nil {
			return fmt.Errorf("sync data for %q: %w", table.Name, err)
		}

		if s.progress != nil {
			s.progress(DataProgress{
				Kind:        DataProgressTableComplete,
				Table:       table.Name,
				TableIndex:  index + 1,
				TotalTables: len(plan.Tables),
				TableRows:   tableRows,
				TotalRows:   totalRows,
				BatchNumber: batchNumber,
			})
		}
	}

	return nil
}
