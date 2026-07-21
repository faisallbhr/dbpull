package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultConfigFileName = "dbpull.yml"
const defaultConfigDirName = "dbpull"

func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return defaultConfigFileName
	}
	return filepath.Join(dir, defaultConfigDirName, defaultConfigFileName)
}

func save(path string, cfg Config, createParent bool) (Config, string, error) {
	prepared, err := Prepare(cfg)
	if err != nil {
		return Config{}, "", err
	}

	if createParent {
		dir := filepath.Dir(path)
		if dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return Config{}, "", fmt.Errorf("create config directory %q: %w", dir, err)
			}
		}
	}

	existingSyncFields := readExistingSyncFields(path)
	fileConfig := configForSave(cfg, prepared, existingSyncFields)
	data, err := yaml.Marshal(fileConfig)
	if err != nil {
		return Config{}, "", fmt.Errorf("generate config file %q: %w", path, err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), ".dbpull-*.yml")
	if err != nil {
		return Config{}, "", fmt.Errorf("create temp config file %q: %w", path, err)
	}

	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return Config{}, "", fmt.Errorf("set temp config permissions %q: %w", tempPath, err)
	}

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return Config{}, "", fmt.Errorf("write temp config file %q: %w", tempPath, err)
	}

	if err := tempFile.Close(); err != nil {
		return Config{}, "", fmt.Errorf("close temp config file %q: %w", tempPath, err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return Config{}, "", fmt.Errorf("replace config file %q: %w", path, err)
	}
	cleanup = false

	if err := os.Chmod(path, 0o600); err != nil {
		return Config{}, "", fmt.Errorf("set config permissions %q: %w", path, err)
	}

	loaded, err := Load(path)
	if err != nil {
		return Config{}, "", err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, "", fmt.Errorf("resolve config file path %q: %w", path, err)
	}

	return loaded, absPath, nil
}

func configForSave(original Config, prepared Config, existingSyncFields map[string]struct{}) Config {
	if (original.Sync.BatchSize == 0 || original.Sync.BatchSize == defaultBatchSize) && !syncFieldExists(existingSyncFields, "batch_size") {
		prepared.Sync.BatchSize = 0
	}
	if (original.Sync.Workers == 0 || original.Sync.Workers == defaultWorkers) && !syncFieldExists(existingSyncFields, "workers") {
		prepared.Sync.Workers = 0
	}
	if (original.Sync.TransactionBatches == 0 || original.Sync.TransactionBatches == defaultTransactionBatches) && !syncFieldExists(existingSyncFields, "transaction_batches") {
		prepared.Sync.TransactionBatches = 0
	}
	if (original.Sync.MaxBatchBytes == 0 || original.Sync.MaxBatchBytes == defaultMaxBatchBytes) && !syncFieldExists(existingSyncFields, "max_batch_bytes") {
		prepared.Sync.MaxBatchBytes = 0
	}
	return prepared
}

func readExistingSyncFields(path string) map[string]struct{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var root struct {
		Sync map[string]any `yaml:"sync"`
	}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	fields := make(map[string]struct{}, len(root.Sync))
	for name := range root.Sync {
		fields[name] = struct{}{}
	}
	return fields
}

func syncFieldExists(fields map[string]struct{}, name string) bool {
	_, ok := fields[name]
	return ok
}

func DefaultInitConfig() Config {
	return Config{
		SSH: SSHConfig{
			Port:       defaultSSHPort,
			PrivateKey: "~/.ssh/id_rsa",
		},
		Target: TargetConfig{
			Host:     "127.0.0.1",
			Port:     defaultTargetPort,
			Username: "root",
		},
		Sync: SyncConfig{
			ExcludeData: []string{
				"failed_jobs",
				"jobs",
				"job_batches",
				"audits",
				"*_temps",
				"telescope_*",
			},
		},
	}
}

func resolvePath(path string) (string, error) {
	if path == "" {
		path = DefaultPath()
	}
	return expandHome(path)
}
