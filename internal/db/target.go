package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/faisallbhr/dbpull/internal/config"

	mysql "github.com/go-sql-driver/mysql"
)

type TargetClient struct {
	db       *sql.DB
	session  targetSession
	closeDB  func() error
	closeSes func() error
	newSes   func(context.Context) (targetSession, func() error, error)

	originalSQLMode          string
	foreignKeyChecksDisabled bool
	syncPrepared             bool
}

type targetSession interface {
	PingContext(ctx context.Context) error
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) targetScanner
	BeginTx(ctx context.Context, opts *sql.TxOptions) (targetTransaction, error)
}

type targetScanner interface {
	Scan(dest ...any) error
}

type targetTransaction interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Commit() error
	Rollback() error
}

type DataSession interface {
	BeginTx(ctx context.Context) (DataTx, error)
	Close(ctx context.Context) error
}

type DataTx interface {
	InsertBatch(ctx context.Context, table string, batch RowBatch) error
	Commit() error
	Rollback() error
}

type sqlTargetSession struct {
	*sql.Conn
}

func (s sqlTargetSession) QueryRowContext(ctx context.Context, query string, args ...any) targetScanner {
	return s.Conn.QueryRowContext(ctx, query, args...)
}

func (s sqlTargetSession) BeginTx(ctx context.Context, opts *sql.TxOptions) (targetTransaction, error) {
	return s.Conn.BeginTx(ctx, opts)
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
	configurePool(db, config.DefaultWorkers())

	return &TargetClient{
		db:      db,
		closeDB: db.Close,
		newSes: func(ctx context.Context) (targetSession, func() error, error) {
			session, err := db.Conn(ctx)
			if err != nil {
				return nil, nil, err
			}
			return sqlTargetSession{Conn: session}, session.Close, nil
		},
	}, nil
}

func (c *TargetClient) Close() error {
	var err error

	err = errorsJoin(err, c.CloseSyncSession(context.Background()))

	if c.closeDB != nil {
		if closeErr := c.closeDB(); closeErr != nil {
			err = errorsJoin(err, fmt.Errorf("close target database: %w", closeErr))
		}
	}

	return err
}

func (c *TargetClient) Ping(ctx context.Context) error {
	if c.db != nil {
		if err := c.db.PingContext(ctx); err != nil {
			return fmt.Errorf("ping target database: %w", err)
		}
		return nil
	}
	if err := c.session.PingContext(ctx); err != nil {
		return fmt.Errorf("ping target database: %w", err)
	}
	return nil
}

func (c *TargetClient) SetPoolSize(workers int) {
	if c.db == nil {
		return
	}
	configurePool(c.db, workers)
}

func (c *TargetClient) PrepareSyncSession(ctx context.Context) error {
	if c.syncPrepared {
		return nil
	}
	if err := c.ensureSession(ctx); err != nil {
		return err
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

func (c *TargetClient) CloseSyncSession(ctx context.Context) error {
	var err error

	if c.syncPrepared {
		err = errorsJoin(err, c.restoreSyncSession(ctx))
	}

	if c.closeSes != nil {
		if closeErr := c.closeSes(); closeErr != nil {
			err = errorsJoin(err, fmt.Errorf("close target database session: %w", closeErr))
		}
	}

	c.session = nil
	c.closeSes = nil
	return err
}

func (c *TargetClient) NewSession(ctx context.Context) (DataSession, error) {
	if c.newSes == nil {
		return &TargetSession{session: c.session}, nil
	}

	session, closeSession, err := c.newSes(ctx)
	if err != nil {
		return nil, fmt.Errorf("open target database session: %w", err)
	}

	worker := &TargetSession{
		session: session,
		close:   closeSession,
	}
	if err := worker.PrepareSyncSession(ctx); err != nil {
		closeErr := worker.Close(context.Background())
		if closeErr != nil {
			return nil, errorsJoin(err, closeErr)
		}
		return nil, err
	}
	return worker, nil
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
	if err := validateBatch(table, batch); err != nil {
		return err
	}
	if len(batch.Rows) == 0 {
		return nil
	}
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	return insertBatchWithSplit(ctx, c.session, table, batch)
}

func (c *TargetClient) ensureSession(ctx context.Context) error {
	if c.session != nil {
		return nil
	}
	if c.newSes == nil {
		return fmt.Errorf("target database session is not available")
	}
	session, closeSession, err := c.newSes(ctx)
	if err != nil {
		return fmt.Errorf("open target database session: %w", err)
	}
	c.session = session
	c.closeSes = closeSession
	return nil
}

func insertBatchWithSplit(ctx context.Context, execer batchExecer, table string, batch RowBatch) error {
	if err := validateBatch(table, batch); err != nil {
		return err
	}
	if len(batch.Rows) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(batch.Rows))
	args := make([]any, 0, len(batch.Rows)*len(batch.Columns))
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(batch.Columns)), ", ") + ")"

	for _, row := range batch.Rows {
		placeholders = append(placeholders, rowPlaceholder)
		args = append(args, row...)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s",
		quoteIdentifier(table),
		quoteIdentifiers(batch.Columns),
		strings.Join(placeholders, ", "),
	)

	if _, err := execer.ExecContext(ctx, query, args...); err != nil {
		wrapped := fmt.Errorf(
			"insert batch into %q for rows %d-%d and columns (%s): %w",
			table,
			0,
			len(batch.Rows)-1,
			quoteIdentifiers(batch.Columns),
			err,
		)
		if isPacketSizeError(err) {
			if len(batch.Rows) == 1 {
				return fmt.Errorf("row on table %q is too large for MySQL target; check target max_allowed_packet or sync.max_batch_bytes: %w", table, wrapped)
			}

			mid := len(batch.Rows) / 2
			if err := insertBatchWithSplit(ctx, execer, table, RowBatch{Columns: batch.Columns, Rows: batch.Rows[:mid]}); err != nil {
				return err
			}
			return insertBatchWithSplit(ctx, execer, table, RowBatch{Columns: batch.Columns, Rows: batch.Rows[mid:]})
		}
		return wrapped
	}

	return nil
}

func validateBatch(table string, batch RowBatch) error {
	if len(batch.Rows) == 0 {
		return nil
	}
	if len(batch.Columns) == 0 {
		return fmt.Errorf("insert batch into %q: columns are required", table)
	}
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
	}
	return nil
}

type batchExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type TargetSession struct {
	session                  targetSession
	close                    func() error
	originalSQLMode          string
	foreignKeyChecksDisabled bool
	syncPrepared             bool
}

func (s *TargetSession) PrepareSyncSession(ctx context.Context) error {
	if s.syncPrepared {
		return nil
	}

	mode, err := currentSQLMode(ctx, s.session)
	if err != nil {
		return fmt.Errorf("read target sql_mode: %w", err)
	}

	if err := setSQLMode(ctx, s.session, addSQLMode(mode, "NO_AUTO_VALUE_ON_ZERO")); err != nil {
		return fmt.Errorf("enable NO_AUTO_VALUE_ON_ZERO: %w", err)
	}

	s.originalSQLMode = mode
	if _, err := s.session.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		restoreErr := setSQLMode(ctx, s.session, mode)
		if restoreErr != nil {
			return errorsJoin(fmt.Errorf("disable foreign key checks: %w", err), fmt.Errorf("restore target sql_mode: %w", restoreErr))
		}
		return fmt.Errorf("disable foreign key checks: %w", err)
	}

	s.foreignKeyChecksDisabled = true
	s.syncPrepared = true
	return nil
}

func (s *TargetSession) BeginTx(ctx context.Context) (DataTx, error) {
	tx, err := s.session.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin target transaction: %w", err)
	}
	return &TargetTx{tx: tx}, nil
}

func (s *TargetSession) InsertBatch(ctx context.Context, table string, batch RowBatch) error {
	return insertBatchWithSplit(ctx, s.session, table, batch)
}

func (s *TargetSession) Close(ctx context.Context) error {
	var err error
	if s.syncPrepared {
		if s.foreignKeyChecksDisabled {
			if _, restoreErr := s.session.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); restoreErr != nil {
				err = errorsJoin(err, fmt.Errorf("enable foreign key checks: %w", restoreErr))
			}
			s.foreignKeyChecksDisabled = false
		}
		if restoreErr := setSQLMode(ctx, s.session, s.originalSQLMode); restoreErr != nil {
			err = errorsJoin(err, fmt.Errorf("restore target sql_mode: %w", restoreErr))
		}
		s.syncPrepared = false
	}
	if s.close != nil {
		err = errorsJoin(err, s.close())
	}
	return err
}

type TargetTx struct {
	tx targetTransaction
}

func (tx *TargetTx) InsertBatch(ctx context.Context, table string, batch RowBatch) error {
	return insertBatchWithSplit(ctx, tx.tx, table, batch)
}

func (tx *TargetTx) Commit() error {
	return tx.tx.Commit()
}

func (tx *TargetTx) Rollback() error {
	return tx.tx.Rollback()
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
		InterpolateParams:    true,
		ParseTime:            true,
	}

	return dsn.FormatDSN(), nil
}

func (c *TargetClient) exec(ctx context.Context, action, query string) error {
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	if _, err := c.session.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func (c *TargetClient) currentSQLMode(ctx context.Context) (string, error) {
	return currentSQLMode(ctx, c.session)
}

func (c *TargetClient) setSQLMode(ctx context.Context, mode string) error {
	return setSQLMode(ctx, c.session, mode)
}

func currentSQLMode(ctx context.Context, session targetSession) (string, error) {
	var mode string
	if err := session.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

func setSQLMode(ctx context.Context, session targetSession, mode string) error {
	if _, err := session.ExecContext(ctx, "SET SESSION sql_mode = ?", mode); err != nil {
		return err
	}
	return nil
}

func isPacketSizeError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && (mysqlErr.Number == 1153 || mysqlErr.Number == 2020) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "max_allowed_packet") ||
		strings.Contains(message, "packet for query is too large") ||
		strings.Contains(message, "packet bigger")
}

func configurePool(db *sql.DB, workers int) {
	if workers < 1 {
		workers = 1
	}
	db.SetMaxOpenConns(workers)
	db.SetMaxIdleConns(workers)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
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
