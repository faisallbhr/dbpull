package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faisallbhr/dbpull/internal/config"
)

func TestRunInitReusesConfigEditor(t *testing.T) {
	prev := runConfigEditor
	prevConfigPath := configPath
	t.Cleanup(func() {
		runConfigEditor = prev
		configPath = prevConfigPath
	})

	called := false
	runConfigEditor = func(opts config.EditorOptions) (config.SessionResult, error) {
		called = true
		wantPath := config.DefaultPath()
		if opts.Path != wantPath {
			t.Fatalf("EditorOptions.Path = %q, want %q", opts.Path, wantPath)
		}
		if !opts.CreateParent {
			t.Fatal("EditorOptions.CreateParent = false, want true")
		}
		return config.SessionResult{Saved: true, Path: opts.Path}, nil
	}

	configPath = config.DefaultPath()
	cmd := newInitCmd()
	if err := runInit(cmd, "", false); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if !called {
		t.Fatal("runConfigEditor was not called")
	}
}

func TestRunConfigUsesConfigEditor(t *testing.T) {
	prev := runConfigEditor
	t.Cleanup(func() { runConfigEditor = prev })

	called := false
	runConfigEditor = func(opts config.EditorOptions) (config.SessionResult, error) {
		called = true
		return config.SessionResult{}, nil
	}

	cmd := newConfigCmd()
	if err := runConfig(cmd, nil); err != nil {
		t.Fatalf("runConfig() error = %v", err)
	}
	if !called {
		t.Fatal("runConfigEditor was not called")
	}
}

func TestCommandsUseConfigFlagPath(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T) error
	}{
		{
			name: "doctor",
			run: func(t *testing.T) error {
				cmd := newDoctorCmd()
				cmd.SetOut(&bytes.Buffer{})
				return runDoctor(cmd, nil)
			},
		},
		{
			name: "plan",
			run: func(t *testing.T) error {
				cmd := newPlanCmd()
				cmd.SetOut(&bytes.Buffer{})
				return runPlan(cmd, nil)
			},
		},
		{
			name: "list-tables",
			run: func(t *testing.T) error {
				cmd := newListTablesCmd()
				cmd.SetOut(&bytes.Buffer{})
				return runListTables(cmd, nil)
			},
		},
		{
			name: "sync",
			run: func(t *testing.T) error {
				cmd := newSyncCmd()
				cmd.SetOut(&bytes.Buffer{})
				return runSync(cmd, nil, false)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prevLoadConfig := loadConfig
			prevConfigPath := configPath
			t.Cleanup(func() {
				loadConfig = prevLoadConfig
				configPath = prevConfigPath
			})

			configPath = "/tmp/custom-dbpull.yml"
			loadConfig = func(path string) (config.Config, error) {
				if path != configPath {
					t.Fatalf("loadConfig path = %q, want %q", path, configPath)
				}
				return config.Config{}, errors.New("stop")
			}

			if err := tt.run(t); err == nil || err.Error() != "stop" {
				t.Fatalf("error = %v, want stop", err)
			}
		})
	}
}
