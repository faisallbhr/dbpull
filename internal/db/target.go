package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"

	"dbpull/internal/config"

	mysql "github.com/go-sql-driver/mysql"
)

type TargetClient struct {
	db    execPinger
	close func() error
}

type execPinger interface {
	PingContext(ctx context.Context) error
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func ConnectTarget(cfg config.TargetConfig) (*TargetClient, error) {
	dsn, err := buildTargetDSN(cfg)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open target database: %w", err)
	}

	return &TargetClient{
		db:    db,
		close: db.Close,
	}, nil
}

func (c *TargetClient) Close() error {
	if c.close == nil {
		return nil
	}
	return c.close()
}

func (c *TargetClient) Ping(ctx context.Context) error {
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping target database: %w", err)
	}
	return nil
}

func (c *TargetClient) DisableForeignKeyChecks(ctx context.Context) error {
	return c.exec(ctx, "disable foreign key checks", "SET FOREIGN_KEY_CHECKS = 0")
}

func (c *TargetClient) EnableForeignKeyChecks(ctx context.Context) error {
	return c.exec(ctx, "enable foreign key checks", "SET FOREIGN_KEY_CHECKS = 1")
}

func (c *TargetClient) DropTable(ctx context.Context, table string) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteIdentifier(table))
	return c.exec(ctx, fmt.Sprintf("drop table %q", table), query)
}

func (c *TargetClient) CreateTable(ctx context.Context, createSQL string) error {
	return c.exec(ctx, "create table", createSQL)
}

func (c *TargetClient) InsertBatch(ctx context.Context, table string, batch RowBatch) error {
	if len(batch.Rows) == 0 {
		return nil
	}

	if len(batch.Columns) == 0 {
		return fmt.Errorf("insert batch into %q: columns are required", table)
	}

	placeholders := make([]string, 0, len(batch.Rows))
	args := make([]any, 0, len(batch.Rows)*len(batch.Columns))
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(batch.Columns)), ", ") + ")"

	for rowIndex, row := range batch.Rows {
		if len(row) != len(batch.Columns) {
			return fmt.Errorf(
				"insert batch into %q: row %d has %d values for %d columns (%s)",
				table,
				rowIndex,
				len(row),
				len(batch.Columns),
				quoteIdentifiers(batch.Columns),
			)
		}

		placeholders = append(placeholders, rowPlaceholder)
		args = append(args, row...)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s",
		quoteIdentifier(table),
		quoteIdentifiers(batch.Columns),
		strings.Join(placeholders, ", "),
	)

	if _, err := c.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf(
			"insert batch into %q for rows %d-%d and columns (%s): %w",
			table,
			0,
			len(batch.Rows)-1,
			quoteIdentifiers(batch.Columns),
			err,
		)
	}

	return nil
}

func buildTargetDSN(cfg config.TargetConfig) (string, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return "", fmt.Errorf("target host is required")
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return "", fmt.Errorf("invalid target address %q: %w", addr, err)
	}

	dsn := mysql.Config{
		User:                 cfg.Username,
		Passwd:               cfg.Password,
		Net:                  "tcp",
		Addr:                 addr,
		DBName:               cfg.Database,
		AllowNativePasswords: true,
		ParseTime:            true,
	}

	return dsn.FormatDSN(), nil
}

func (c *TargetClient) exec(ctx context.Context, action, query string) error {
	if _, err := c.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func quoteIdentifiers(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, quoteIdentifier(name))
	}
	return strings.Join(quoted, ", ")
}
