package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultSSHPort    = 22
	defaultTargetPort = 3306
	defaultBatchSize  = 1000
)

type Config struct {
	Source SourceConfig `yaml:"source"`
	SSH    SSHConfig    `yaml:"ssh"`
	Target TargetConfig `yaml:"target"`
	Sync   SyncConfig   `yaml:"sync"`
}

type SourceConfig struct {
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type SSHConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	User       string `yaml:"user"`
	PrivateKey string `yaml:"private_key"`
}

type TargetConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type SyncConfig struct {
	BatchSize     int      `yaml:"batch_size"`
	ExcludeTables []string `yaml:"exclude_tables"`
	ExcludeData   []string `yaml:"exclude_data"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg, err := parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", path, err)
	}

	return cfg, nil
}

func parse(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return Prepare(cfg)
}

func validate(cfg Config) error {
	required := []struct {
		value string
		field string
	}{
		{cfg.Source.Database, "source.database"},
		{cfg.Source.Username, "source.username"},
		{cfg.Source.Password, "source.password"},
		{cfg.SSH.Host, "ssh.host"},
		{cfg.SSH.User, "ssh.user"},
		{cfg.SSH.PrivateKey, "ssh.private_key"},
		{cfg.Target.Host, "target.host"},
		{cfg.Target.Database, "target.database"},
		{cfg.Target.Username, "target.username"},
		{cfg.Target.Password, "target.password"},
	}

	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("missing required field %q", field.field)
		}
	}

	if cfg.SSH.Port <= 0 {
		return fmt.Errorf("invalid value for %q: must be greater than 0", "ssh.port")
	}

	if cfg.Target.Port <= 0 {
		return fmt.Errorf("invalid value for %q: must be greater than 0", "target.port")
	}

	if cfg.Sync.BatchSize <= 0 {
		return fmt.Errorf("invalid value for %q: must be greater than 0", "sync.batch_size")
	}

	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.SSH.Port == 0 {
		cfg.SSH.Port = defaultSSHPort
	}

	if cfg.Target.Port == 0 {
		cfg.Target.Port = defaultTargetPort
	}

	if cfg.Sync.BatchSize == 0 {
		cfg.Sync.BatchSize = defaultBatchSize
	}
}

func expandPaths(cfg *Config) error {
	expanded, err := expandHome(cfg.SSH.PrivateKey)
	if err != nil {
		return fmt.Errorf("expand %q: %w", "ssh.private_key", err)
	}

	cfg.SSH.PrivateKey = expanded
	return nil
}

func expandHome(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}

	if path != "~" && !strings.HasPrefix(path, "~/") {
		return "", errors.New("only \"~\" and \"~/...\" paths are supported")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, path[2:]), nil
}
