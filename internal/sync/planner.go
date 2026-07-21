package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/faisallbhr/dbpull/internal/config"
	"github.com/faisallbhr/dbpull/internal/db"
)

type Planner struct {
	cfg    config.Config
	source sourceReader
}

type SyncPlan struct {
	SourceDatabase string
	TargetDatabase string
	Tables         []PlanTable
	Skipped        []SkippedTable
}

type PlanTable struct {
	Name         string
	DataExcluded bool
}

type SkippedTable struct {
	Name   string
	Reason string
}

type sourceReader interface {
	ListTables(ctx context.Context) ([]db.Table, error)
}

func NewPlanner(cfg config.Config, source sourceReader) *Planner {
	return &Planner{
		cfg:    cfg,
		source: source,
	}
}

func (p *Planner) Build(ctx context.Context, selectedTables []string) (SyncPlan, error) {
	tables, err := p.source.ListTables(ctx)
	if err != nil {
		return SyncPlan{}, fmt.Errorf("build sync plan: %w", err)
	}

	filtered, skipped := p.filterTables(tables, selectedTables)

	plan := SyncPlan{
		SourceDatabase: p.cfg.Source.Database,
		TargetDatabase: p.cfg.Target.Database,
		Skipped:        skipped,
		Tables:         make([]PlanTable, 0, len(filtered)),
	}

	if len(filtered) == 0 {
		return plan, nil
	}

	for _, table := range filtered {
		plan.Tables = append(plan.Tables, PlanTable{
			Name:         table.Name,
			DataExcluded: table.DataExcluded,
		})
	}

	return plan, nil
}

func (p *Planner) filterTables(tables []db.Table, selectedTables []string) ([]PlanTable, []SkippedTable) {
	selected := normalizeSelectedTables(selectedTables)
	filtered := make([]PlanTable, 0, len(tables))
	skipped := make([]SkippedTable, 0)

	for _, table := range tables {
		if len(selected) > 0 {
			if _, ok := selected[table.Name]; !ok {
				skipped = append(skipped, SkippedTable{Name: table.Name, Reason: "not selected"})
				continue
			}
		}

		if matchesPattern(table.Name, p.cfg.Sync.ExcludeTables) {
			skipped = append(skipped, SkippedTable{Name: table.Name, Reason: "table excluded"})
			continue
		}

		filtered = append(filtered, PlanTable{
			Name:         table.Name,
			DataExcluded: matchesPattern(table.Name, p.cfg.Sync.ExcludeData),
		})
	}

	sort.Slice(skipped, func(i, j int) bool {
		return skipped[i].Name < skipped[j].Name
	})

	return filtered, skipped
}

func normalizeSelectedTables(selectedTables []string) map[string]struct{} {
	if len(selectedTables) == 0 {
		return nil
	}

	selected := make(map[string]struct{}, len(selectedTables))
	for _, table := range selectedTables {
		name := strings.TrimSpace(table)
		if name == "" {
			continue
		}
		selected[name] = struct{}{}
	}

	return selected
}

func matchesPattern(name string, patterns []string) bool {
	for _, excluded := range patterns {
		if matched, _ := filepath.Match(excluded, name); matched {
			return true
		}
	}

	return false
}
