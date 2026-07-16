package cmd

import (
	"context"
	"time"

	"dbpull/internal/config"
	"dbpull/internal/db"
	"dbpull/internal/ssh"
	syncpkg "dbpull/internal/sync"
)

const (
	defaultSourceDBAddr = "127.0.0.1:3306"
	doctorTimeout       = 10 * time.Second
)

type sourceClient interface {
	Close() error
	Ping(ctx context.Context) error
	ListTables(ctx context.Context) ([]db.Table, error)
	ShowCreateTable(ctx context.Context, table string) (string, error)
	StreamRows(
		ctx context.Context,
		table string,
		batchSize int,
		notice db.BatchSizeNotice,
		handle db.RowBatchHandler,
	) error
}

type targetClient interface {
	Close() error
	Ping(ctx context.Context) error
	PrepareSyncSession(ctx context.Context) error
	DropTable(ctx context.Context, table string) error
	CreateTable(ctx context.Context, createSQL string) error
	InsertBatch(ctx context.Context, table string, batch db.RowBatch) error
}

var (
	loadConfig    = config.Load
	newTunnel     = func(cfg config.SSHConfig, remoteAddr string) ssh.Tunnel { return ssh.NewTunnel(cfg, remoteAddr) }
	connectSource = func(cfg config.SourceConfig, tunnelAddr string) (sourceClient, error) {
		return db.ConnectSource(cfg, tunnelAddr)
	}
	connectTarget = func(cfg config.TargetConfig) (targetClient, error) {
		return db.ConnectTarget(cfg)
	}
	newPlanner = func(cfg config.Config, source sourceClient) *syncpkg.Planner {
		return syncpkg.NewPlanner(cfg, source)
	}
	newSchemaSyncer = func(source sourceClient, target targetClient) *syncpkg.SchemaSyncer {
		return syncpkg.NewSchemaSyncer(source, target)
	}
	newDataSyncer = func(source sourceClient, target targetClient, progress func(syncpkg.DataProgress)) *syncpkg.DataSyncer {
		return syncpkg.NewDataSyncer(source, target, progress)
	}
)
