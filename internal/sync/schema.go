package sync

import (
	"context"
	"fmt"
)

type SchemaSyncer struct {
	source schemaSource
	target schemaTarget
}

type schemaSource interface {
	ShowCreateTable(ctx context.Context, table string) (string, error)
}

type schemaTarget interface {
	DropTable(ctx context.Context, table string) error
	CreateTable(ctx context.Context, createSQL string) error
}

func NewSchemaSyncer(source schemaSource, target schemaTarget) *SchemaSyncer {
	return &SchemaSyncer{
		source: source,
		target: target,
	}
}

func (s *SchemaSyncer) Sync(ctx context.Context, plan SyncPlan) error {
	for _, table := range plan.Tables {
		createSQL, err := s.source.ShowCreateTable(ctx, table.Name)
		if err != nil {
			return fmt.Errorf("sync schema for %q: %w", table.Name, err)
		}

		if err := s.target.DropTable(ctx, table.Name); err != nil {
			return fmt.Errorf("sync schema for %q: %w", table.Name, err)
		}

		if err := s.target.CreateTable(ctx, createSQL); err != nil {
			return fmt.Errorf("sync schema for %q: %w", table.Name, err)
		}
	}

	return nil
}
