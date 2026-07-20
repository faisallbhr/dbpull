package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadParsesResolvesAndAppliesDefaults(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: olshoperp
  username: remote_db
  password: source-secret

ssh:
  host: olshoperp.com
  user: remote_db
  private_key: ~/.ssh/id_rsa

target:
  host: localhost
  database: olshoperp_local
  username: root
  password: target-secret

sync:
  exclude_data:
    - failed_jobs
    - "*_logs"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}

	if cfg.SSH.PrivateKey != filepath.Join(home, ".ssh/id_rsa") {
		t.Fatalf("SSH.PrivateKey = %q", cfg.SSH.PrivateKey)
	}

	if cfg.SSH.Port != defaultSSHPort {
		t.Fatalf("SSH.Port = %d, want %d", cfg.SSH.Port, defaultSSHPort)
	}

	if cfg.Target.Port != defaultTargetPort {
		t.Fatalf("Target.Port = %d, want %d", cfg.Target.Port, defaultTargetPort)
	}

	if cfg.Sync.BatchSize != defaultBatchSize {
		t.Fatalf("Sync.BatchSize = %d, want %d", cfg.Sync.BatchSize, defaultBatchSize)
	}
	if cfg.Sync.Workers != defaultWorkers {
		t.Fatalf("Sync.Workers = %d, want %d", cfg.Sync.Workers, defaultWorkers)
	}
	if cfg.Sync.TransactionBatches != defaultTransactionBatches {
		t.Fatalf("Sync.TransactionBatches = %d, want %d", cfg.Sync.TransactionBatches, defaultTransactionBatches)
	}
	if cfg.Sync.MaxBatchBytes != defaultMaxBatchBytes {
		t.Fatalf("Sync.MaxBatchBytes = %d, want %d", cfg.Sync.MaxBatchBytes, defaultMaxBatchBytes)
	}

	if got, want := cfg.Sync.ExcludeData, []string{"failed_jobs", "*_logs"}; !slicesEqual(got, want) {
		t.Fatalf("Sync.ExcludeData = %#v, want %#v", got, want)
	}
}

func TestLoadReadsManualAdvancedSyncConfig(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: olshoperp
  username: remote_db
  password: source-secret
ssh:
  host: olshoperp.com
  user: remote_db
  private_key: ~/.ssh/id_rsa
target:
  host: localhost
  database: olshoperp_local
  username: root
  password: target-secret
sync:
  workers: 4
  transaction_batches: 7
  max_batch_bytes: 1048576
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Sync.Workers != 4 || cfg.Sync.TransactionBatches != 7 || cfg.Sync.MaxBatchBytes != 1048576 {
		t.Fatalf("Sync = %#v", cfg.Sync)
	}
}

func TestLoadReturnsInvalidAdvancedSyncError(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: olshoperp
  username: remote_db
  password: source-secret
ssh:
  host: olshoperp.com
  user: remote_db
  private_key: ~/.ssh/id_rsa
target:
  host: localhost
  database: olshoperp_local
  username: root
  password: target-secret
sync:
  workers: 0
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), `invalid value for "sync.workers": must be greater than 0`) {
		t.Fatalf("Load() error = %q", err)
	}
}

func TestLoadReturnsValidationErrorForMissingRequiredField(t *testing.T) {
	path := writeConfigFile(t, `
source:
  username: remote_db
  password: source-secret

ssh:
  host: olshoperp.com
  user: remote_db
  private_key: ~/.ssh/id_rsa

target:
  host: localhost
  database: olshoperp_local
  username: root
  password: target-secret
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}

	if !strings.Contains(err.Error(), `missing required field "source.database"`) {
		t.Fatalf("Load() error = %q", err)
	}
}

func TestLoadReturnsValidationErrorForMissingPassword(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: olshoperp
  username: remote_db

ssh:
  host: olshoperp.com
  user: remote_db
  private_key: ~/.ssh/id_rsa

target:
  host: localhost
  database: olshoperp_local
  username: root
  password: target-secret
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}

	if !strings.Contains(err.Error(), `missing required field "source.password"`) {
		t.Fatalf("Load() error = %q", err)
	}
}

func TestLoadReturnsPathExpansionError(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: olshoperp
  username: remote_db
  password: source-secret

ssh:
  host: olshoperp.com
  user: remote_db
  private_key: ~other/.ssh/id_rsa

target:
  host: localhost
  database: olshoperp_local
  username: root
  password: target-secret
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want path expansion error")
	}

	if !strings.Contains(err.Error(), `expand "ssh.private_key": only "~" and "~/..." paths are supported`) {
		t.Fatalf("Load() error = %q", err)
	}
}

func TestLoadReturnsInvalidBatchSizeError(t *testing.T) {
	path := writeConfigFile(t, `
source:
  database: olshoperp
  username: remote_db
  password: source-secret

ssh:
  host: olshoperp.com
  user: remote_db
  private_key: ~/.ssh/id_rsa

target:
  host: localhost
  database: olshoperp_local
  username: root
  password: target-secret

sync:
  batch_size: -1
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}

	if !strings.Contains(err.Error(), `invalid value for "sync.batch_size": must be greater than 0`) {
		t.Fatalf("Load() error = %q", err)
	}
}

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "dbpull.yml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	return path
}

func slicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
