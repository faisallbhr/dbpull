package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"

	"dbpull/internal/config"

	mysql "github.com/go-sql-driver/mysql"
)

type TargetClient struct {
	session  targetSession
	closeDB  func() error
	closeSes func() error

	originalSQLMode          string
	foreignKeyChecksDisabled bool
	syncPrepared             bool
}

type targetSession interface {
	PingContext(ctx context.Context) error
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) targetScanner
}

type targetScanner interface {
	Scan(dest ...any) error
}

type sqlTargetSession struct {
	*sql.Conn
}

func (s sqlTargetSession) QueryRowContext(ctx context.Context, query string, args ...any) targetScanner {
	return s.Conn.QueryRowContext(ctx, query, args...)
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

	session, err := db.Conn(context.Background())
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open target database session: %w", err)
	}

	return &TargetClient{
		session:  sqlTargetSession{Conn: session},
		closeDB:  db.Close,
		closeSes: session.Close,
	}, nil
}

func (c *TargetClient) Close() error {
	var err error

	if c.syncPrepared {
		if restoreErr := c.restoreSyncSession(context.Background()); restoreErr != nil {
			err = restoreErr
		}
	}

	if c.closeSes != nil {
		if closeErr := c.closeSes(); closeErr != nil {
			err = errorsJoin(err, fmt.Errorf("close target database session: %w", closeErr))
		}
	}

	if c.closeDB != nil {
		if closeErr := c.closeDB(); closeErr != nil {
			err = errorsJoin(err, fmt.Errorf("close target database: %w", closeErr))
		}
	}

	return err
}

func (c *TargetClient) Ping(ctx context.Context) error {
	if err := c.session.PingContext(ctx); err != nil {
		return fmt.Errorf("ping target database: %w", err)
	}
	return nil
}

func (c *TargetClient) PrepareSyncSession(ctx context.Context) error {
	if c.syncPrepared {
		return nil
	}

	mode, err := c.currentSQLMode(ctx)
	if err != nil {
		return fmt.Errorf("read target sql_mode: %w", err)
	}

	if err := c.setSQLMode(ctx, addSQLMode(mode, "NO_AUTO_VALUE_ON_ZERO")); err != nil {
		return fmt.Errorf("enable NO_AUTO_VALUE_ON_ZERO: %w", err)
	}

	c.originalSQLMode = mode
	if err := c.DisableForeignKeyChecks(ctx); err != nil {
		restoreErr := c.setSQLMode(ctx, mode)
		if restoreErr != nil {
			return errorsJoin(err, fmt.Errorf("restore target sql_mode: %w", restoreErr))
		}
		return err
	}

	c.foreignKeyChecksDisabled = true
	c.syncPrepared = true
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

	if _, err := c.session.ExecContext(ctx, query, args...); err != nil {
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

func (c *TargetClient) restoreSyncSession(ctx context.Context) error {
	var err error

	if c.foreignKeyChecksDisabled {
		if restoreErr := c.EnableForeignKeyChecks(ctx); restoreErr != nil {
			err = restoreErr
		}
		c.foreignKeyChecksDisabled = false
	}

	if restoreErr := c.setSQLMode(ctx, c.originalSQLMode); restoreErr != nil {
		err = errorsJoin(err, fmt.Errorf("restore target sql_mode: %w", restoreErr))
	}

	c.syncPrepared = false
	return err
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
	if _, err := c.session.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func (c *TargetClient) currentSQLMode(ctx context.Context) (string, error) {
	var mode string
	if err := c.session.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

func (c *TargetClient) setSQLMode(ctx context.Context, mode string) error {
	if _, err := c.session.ExecContext(ctx, "SET SESSION sql_mode = ?", mode); err != nil {
		return err
	}
	return nil
}

func addSQLMode(mode, required string) string {
	parts := strings.Split(mode, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == required {
			return mode
		}
	}
	if strings.TrimSpace(mode) == "" {
		return required
	}
	return mode + "," + required
}

func errorsJoin(current, next error) error {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	return errors.Join(current, next)
}

func quoteIdentifiers(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, quoteIdentifier(name))
	}
	return strings.Join(quoted, ", ")
}
