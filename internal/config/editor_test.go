package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() Config {
	cfg := DefaultInitConfig()
	cfg.Source.Database = "app"
	cfg.Source.Username = "user"
	cfg.Source.Password = "pass"
	cfg.SSH.Host = "ssh.example.com"
	cfg.SSH.User = "remote"
	cfg.Target.Database = "app_local"
	cfg.Target.Password = "root"
	return cfg
}

func TestRunEditorLoadsExistingConfig(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: app
  username: user
  password: pass
ssh:
  host: ssh.example.com
  port: 22
  user: remote
  private_key: ~/.ssh/id_rsa
target:
  host: 127.0.0.1
  port: 3306
  database: app_local
  username: root
  password: root
sync:
  batch_size: 1000
  exclude_tables: []
  exclude_data:
    - audits
`)

	result, err := runEditor(newFakeUI(
		[]string{menuExit},
		nil,
	), EditorOptions{Path: path})
	if err != nil {
		t.Fatalf("runEditor() error = %v", err)
	}

	if !result.Loaded {
		t.Fatal("result.Loaded = false, want true")
	}
	if result.Config.Source.Database != "app" {
		t.Fatalf("result.Config.Source.Database = %q", result.Config.Source.Database)
	}
}

func TestRunEditorEditsOneSectionOnly(t *testing.T) {
	initial := DefaultInitConfig()

	result, err := runEditor(newFakeUI(
		[]string{menuSource, menuExit, menuNo},
		[]string{"new_db", "new_user", "new_pass"},
	), EditorOptions{Path: filepath.Join(t.TempDir(), "dbpull.yml")})
	if err != nil {
		t.Fatalf("runEditor() error = %v", err)
	}

	if result.Saved {
		t.Fatal("result.Saved = true, want false")
	}
	if result.Config.Source.Database != initial.Source.Database {
		t.Fatalf("result.Config.Source.Database = %q, want %q", result.Config.Source.Database, initial.Source.Database)
	}
}

func TestRunEditorSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbpull.yml")
	_, _, err := save(path, validConfig(), true)
	if err != nil {
		t.Fatalf("save() error = %v", err)
	}

	result, err := runEditor(newFakeUI(
		[]string{menuSource, menuSave},
		[]string{"app", "user", "pass"},
	), EditorOptions{Path: path, CreateParent: true})
	if err != nil {
		t.Fatalf("runEditor() error = %v", err)
	}

	if !result.Saved {
		t.Fatal("result.Saved = false, want true")
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Source.Database != "app" {
		t.Fatalf("loaded.Source.Database = %q", loaded.Source.Database)
	}
}

func TestRunEditorUnsavedChangesConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbpull.yml")

	result, err := runEditor(newFakeUI(
		[]string{menuSource, menuExit, menuCancel, menuExit, menuNo},
		[]string{"app", "user", "pass"},
	), EditorOptions{Path: path})
	if err != nil {
		t.Fatalf("runEditor() error = %v", err)
	}

	if result.Saved {
		t.Fatal("result.Saved = true, want false")
	}
	if !result.Aborted {
		t.Fatal("result.Aborted = false, want true")
	}
}

func TestRunEditorExcludeTablesAddRemove(t *testing.T) {
	result, err := runEditor(newFakeUI(
		[]string{
			menuSync,
			menuExcludeTable,
			menuAdd,
			menuRemove,
			"jobs",
			menuBack,
			menuBack,
			menuExit,
			menuNo,
		},
		[]string{"jobs"},
	), EditorOptions{Path: filepath.Join(t.TempDir(), "dbpull.yml")})
	if err != nil {
		t.Fatalf("runEditor() error = %v", err)
	}

	if len(result.Config.Sync.ExcludeTables) != 0 {
		t.Fatalf("ExcludeTables = %#v, want empty", result.Config.Sync.ExcludeTables)
	}
}

func TestRunEditorExcludeDataAddRemove(t *testing.T) {
	result, err := runEditor(newFakeUI(
		[]string{
			menuSync,
			menuExcludeData,
			menuAdd,
			menuRemove,
			"audits",
			menuBack,
			menuBack,
			menuExit,
			menuNo,
		},
		[]string{"audits"},
	), EditorOptions{Path: filepath.Join(t.TempDir(), "dbpull.yml")})
	if err != nil {
		t.Fatalf("runEditor() error = %v", err)
	}

	if len(result.Config.Sync.ExcludeData) != len(DefaultInitConfig().Sync.ExcludeData) {
		t.Fatalf("ExcludeData = %#v", result.Config.Sync.ExcludeData)
	}
}

func TestPatternListTitleSummarizesLongLists(t *testing.T) {
	items := []string{
		"failed_jobs",
		"jobs",
		"job_batches",
		"audits",
		"telescope_*",
		"audit_codes",
		"*_export*",
		"*_import*",
		"*_log*",
	}

	title := patternListTitle("Exclude Data", items)

	if strings.Contains(title, "*_log*") {
		t.Fatalf("patternListTitle() showed hidden item:\n%s", title)
	}
	if !strings.Contains(title, "... and 1 more") {
		t.Fatalf("patternListTitle() missing summary:\n%s", title)
	}
}

func TestRunEditorDoesNotShowAdvancedSyncFields(t *testing.T) {
	ui := newFakeUI(
		[]string{menuSync, menuBack, menuExit},
		nil,
	)

	_, err := runEditor(ui, EditorOptions{Path: filepath.Join(t.TempDir(), "dbpull.yml")})
	if err != nil {
		t.Fatalf("runEditor() error = %v", err)
	}

	got := strings.Join(ui.optionLabels, "\n")
	for _, advanced := range []string{"workers", "transaction", "bytes", "packet"} {
		if strings.Contains(strings.ToLower(got), advanced) {
			t.Fatalf("advanced field %q shown in options:\n%s", advanced, got)
		}
	}
}

func TestSaveDoesNotWriteDefaultAdvancedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbpull.yml")

	_, _, err := save(path, validConfig(), true)
	if err != nil {
		t.Fatalf("save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	text := string(data)
	for _, field := range []string{"workers:", "transaction_batches:", "max_batch_bytes:"} {
		if strings.Contains(text, field) {
			t.Fatalf("config contains advanced field %q:\n%s", field, text)
		}
	}
}

func TestSavePreservesManualAdvancedFields(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: app
  username: user
  password: pass
ssh:
  host: ssh.example.com
  port: 22
  user: remote
  private_key: ~/.ssh/id_rsa
target:
  host: 127.0.0.1
  port: 3306
  database: app_local
  username: root
  password: root
sync:
  batch_size: 1000
  workers: 4
  transaction_batches: 10
  max_batch_bytes: 1048576
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Source.Database = "renamed"
	if _, _, err := save(path, cfg, false); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	text := string(data)
	for _, field := range []string{"workers: 4", "transaction_batches: 10", "max_batch_bytes: 1048576"} {
		if !strings.Contains(text, field) {
			t.Fatalf("config missing %q:\n%s", field, text)
		}
	}
}

func TestRunEditorValidation(t *testing.T) {
	_, err := runEditor(newFakeUI(
		[]string{menuSource, menuSave},
		[]string{"", "user", "pass"},
	), EditorOptions{Path: filepath.Join(t.TempDir(), "dbpull.yml")})
	if err == nil {
		t.Fatal("runEditor() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), `missing required field "source.database"`) {
		t.Fatalf("runEditor() error = %q", err)
	}
}

func TestSaveFileAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbpull.yml")

	_, _, err := save(path, validConfig(), true)
	if err != nil {
		t.Fatalf("save() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dbpull-") {
			t.Fatalf("temp file left behind: %q", entry.Name())
		}
	}
}

func TestSaveFilePreservesPermission0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbpull.yml")

	_, _, err := save(path, validConfig(), true)
	if err != nil {
		t.Fatalf("save() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

type fakeUI struct {
	selects      []string
	inputs       []string
	optionLabels []string
	inputTitles  []string
}

func newFakeUI(selects, inputs []string) *fakeUI {
	return &fakeUI{
		selects: append([]string(nil), selects...),
		inputs:  append([]string(nil), inputs...),
	}
}

func (f *fakeUI) Select(title string, options []MenuOption) (string, error) {
	for _, option := range options {
		f.optionLabels = append(f.optionLabels, option.Label)
	}
	if len(f.selects) == 0 {
		return "", nil
	}
	value := f.selects[0]
	f.selects = f.selects[1:]
	return value, nil
}

func (f *fakeUI) Input(title string, value string, password bool) (string, error) {
	f.inputTitles = append(f.inputTitles, title)
	if len(f.inputs) == 0 {
		return value, nil
	}
	next := f.inputs[0]
	f.inputs = f.inputs[1:]
	return next, nil
}
