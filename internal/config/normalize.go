package config

import "strings"

func Prepare(cfg Config) (Config, error) {
	cfg = trimConfig(cfg)
	cfg.Sync.ExcludeTables = normalizePatterns(cfg.Sync.ExcludeTables)
	cfg.Sync.ExcludeData = normalizePatterns(cfg.Sync.ExcludeData)

	applyDefaults(&cfg)

	if err := expandPaths(&cfg); err != nil {
		return Config{}, err
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func trimConfig(cfg Config) Config {
	cfg.Source.Database = strings.TrimSpace(cfg.Source.Database)
	cfg.Source.Username = strings.TrimSpace(cfg.Source.Username)
	cfg.Source.Password = strings.TrimSpace(cfg.Source.Password)
	cfg.SSH.Host = strings.TrimSpace(cfg.SSH.Host)
	cfg.SSH.User = strings.TrimSpace(cfg.SSH.User)
	cfg.SSH.PrivateKey = strings.TrimSpace(cfg.SSH.PrivateKey)
	cfg.Target.Host = strings.TrimSpace(cfg.Target.Host)
	cfg.Target.Database = strings.TrimSpace(cfg.Target.Database)
	cfg.Target.Username = strings.TrimSpace(cfg.Target.Username)
	cfg.Target.Password = strings.TrimSpace(cfg.Target.Password)
	return cfg
}
