package sync

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSchemaSyncerSync(t *testing.T) {
	var steps []string

	syncer := NewSchemaSyncer(
		fakeSchemaSource{
			showCreateTable: func(ctx context.Context, table string) (string, error) {
				steps = append(steps, "show:"+table)
				return "CREATE TABLE `" + table + "` (`id` bigint)", nil
			},
		},
		fakeSchemaTarget{
			dropTable: func(ctx context.Context, table string) error {
				steps = append(steps, "drop:"+table)
				return nil
			},
			createTable: func(ctx context.Context, createSQL string) error {
				steps = append(steps, "create:"+createSQL)
				return nil
			},
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{
			{Name: "users"},
			{Name: "products"},
		},
	})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	want := []string{
		"show:users",
		"drop:users",
		"create:CREATE TABLE `users` (`id` bigint)",
		"show:products",
		"drop:products",
		"create:CREATE TABLE `products` (`id` bigint)",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("Sync() steps = %#v, want %#v", steps, want)
	}
}

func TestSchemaSyncerStopsOnFirstError(t *testing.T) {
	wantErr := errors.New("boom")

	syncer := NewSchemaSyncer(
		fakeSchemaSource{
			showCreateTable: func(ctx context.Context, table string) (string, error) {
				return "CREATE TABLE `users` (`id` bigint)", nil
			},
		},
		fakeSchemaTarget{
			dropTable: func(ctx context.Context, table string) error { return wantErr },
		},
	)

	err := syncer.Sync(context.Background(), SyncPlan{
		Tables: []PlanTable{{Name: "users"}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Sync() error = %v, want %v", err, wantErr)
	}
}

type fakeSchemaSource struct {
	showCreateTable func(context.Context, string) (string, error)
}

func (f fakeSchemaSource) ShowCreateTable(ctx context.Context, table string) (string, error) {
	if f.showCreateTable != nil {
		return f.showCreateTable(ctx, table)
	}
	return "", nil
}

type fakeSchemaTarget struct {
	dropTable   func(context.Context, string) error
	createTable func(context.Context, string) error
}

func (f fakeSchemaTarget) DropTable(ctx context.Context, table string) error {
	if f.dropTable != nil {
		return f.dropTable(ctx, table)
	}
	return nil
}

func (f fakeSchemaTarget) CreateTable(ctx context.Context, createSQL string) error {
	if f.createTable != nil {
		return f.createTable(ctx, createSQL)
	}
	return nil
}
