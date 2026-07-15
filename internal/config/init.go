package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultConfigFileName = "dbpull.yml"

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

	data, err := yaml.Marshal(prepared)
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
			BatchSize: defaultBatchSize,
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

func resolvePath(path string) string {
	if path == "" {
		return defaultConfigFileName
	}
	return path
}
