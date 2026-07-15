package sync

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dbpull/internal/config"
	"dbpull/internal/db"
)

func TestPlannerBuild(t *testing.T) {
	planner := NewPlanner(config.Config{
		Source: config.SourceConfig{Database: "source_db"},
		Target: config.TargetConfig{Database: "target_db"},
		Sync: config.SyncConfig{
			ExcludeTables: []string{"jobs", "telescope_*"},
			ExcludeData:   []string{"*_logs"},
		},
	}, fakeSourceReader{
		listTables: func(context.Context) ([]db.Table, error) {
			return []db.Table{
				{Name: "jobs"},
				{Name: "products"},
				{Name: "sales_logs"},
				{Name: "telescope_entries"},
				{Name: "users"},
			}, nil
		},
	})

	plan, err := planner.Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if plan.SourceDatabase != "source_db" || plan.TargetDatabase != "target_db" {
		t.Fatalf("Build() databases = %#v", plan)
	}

	wantTables := []PlanTable{
		{Name: "products"},
		{Name: "sales_logs", DataExcluded: true},
		{Name: "users"},
	}
	if !reflect.DeepEqual(plan.Tables, wantTables) {
		t.Fatalf("Build() tables = %#v, want %#v", plan.Tables, wantTables)
	}

	wantSkipped := []SkippedTable{
		{Name: "jobs", Reason: "table excluded"},
		{Name: "telescope_entries", Reason: "table excluded"},
	}
	if !reflect.DeepEqual(plan.Skipped, wantSkipped) {
		t.Fatalf("Build() skipped = %#v, want %#v", plan.Skipped, wantSkipped)
	}
}

func TestPlannerBuildWithSelectedTables(t *testing.T) {
	planner := NewPlanner(config.Config{
		Source: config.SourceConfig{Database: "source_db"},
		Target: config.TargetConfig{Database: "target_db"},
	}, fakeSourceReader{
		listTables: func(context.Context) ([]db.Table, error) {
			return []db.Table{
				{Name: "products"},
				{Name: "users"},
				{Name: "orders"},
			}, nil
		},
	})

	plan, err := planner.Build(context.Background(), []string{"users"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	wantTables := []PlanTable{{Name: "users"}}
	if !reflect.DeepEqual(plan.Tables, wantTables) {
		t.Fatalf("Build() tables = %#v, want %#v", plan.Tables, wantTables)
	}

	wantSkipped := []SkippedTable{
		{Name: "orders", Reason: "not selected"},
		{Name: "products", Reason: "not selected"},
	}
	if !reflect.DeepEqual(plan.Skipped, wantSkipped) {
		t.Fatalf("Build() skipped = %#v, want %#v", plan.Skipped, wantSkipped)
	}
}

func TestPlannerBuildReturnsSourceError(t *testing.T) {
	wantErr := errors.New("boom")
	planner := NewPlanner(config.Config{}, fakeSourceReader{
		listTables: func(context.Context) ([]db.Table, error) {
			return nil, wantErr
		},
	})

	_, err := planner.Build(context.Background(), nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Build() error = %v, want %v", err, wantErr)
	}
}

func TestShouldSkipTable(t *testing.T) {
	cfg := config.SyncConfig{
		ExcludeTables: []string{"telescope_*", "jobs"},
		ExcludeData:   []string{"*_logs"},
	}

	tests := []struct {
		name string
		want bool
	}{
		{name: "jobs", want: true},
		{name: "telescope_entries", want: true},
		{name: "sales_logs", want: true},
		{name: "users", want: false},
	}

	for _, tt := range tests {
		if got := matchesPattern(tt.name, cfg.ExcludeTables) || matchesPattern(tt.name, cfg.ExcludeData); got != tt.want {
			t.Fatalf("match(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

type fakeSourceReader struct {
	listTables func(context.Context) ([]db.Table, error)
}

func (f fakeSourceReader) ListTables(ctx context.Context) ([]db.Table, error) {
	if f.listTables != nil {
		return f.listTables(ctx)
	}
	return nil, nil
}
