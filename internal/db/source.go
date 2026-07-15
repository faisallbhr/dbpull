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

type SourceClient struct {
	db       queryer
	close    func() error
	database string
}

type Table struct {
	Name string
}

type RowBatch struct {
	Columns []string
	Rows    [][]any
}

type RowBatchHandler func(batch RowBatch) error
type BatchSizeNotice func(BatchSizeAdjustment)

type BatchSizeAdjustment struct {
	Table          string
	ConfiguredSize int
	EffectiveSize  int
	ColumnCount    int
}

const safePlaceholderLimit = 60000

type queryer interface {
	PingContext(ctx context.Context) error
	QueryContext(ctx context.Context, query string, args ...any) (rowSet, error)
	QueryRowContext(ctx context.Context, query string, args ...any) scanner
}

type rowSet interface {
	Columns() ([]string, error)
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

type scanner interface {
	Scan(dest ...any) error
}

type sqlDB struct {
	*sql.DB
}

func ConnectSource(cfg config.SourceConfig, tunnelAddr string) (*SourceClient, error) {
	dsn, err := buildSourceDSN(cfg, tunnelAddr)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open source database: %w", err)
	}

	return &SourceClient{
		db:       sqlDB{DB: db},
		close:    db.Close,
		database: cfg.Database,
	}, nil
}

func (c *SourceClient) Close() error {
	if c.close == nil {
		return nil
	}
	return c.close()
}

func (c *SourceClient) Ping(ctx context.Context) error {
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping source database: %w", err)
	}
	return nil
}

func (c *SourceClient) ListTables(ctx context.Context) ([]Table, error) {
	const query = `
SELECT table_name
FROM information_schema.tables
WHERE table_schema = ?
  AND table_type = 'BASE TABLE'
ORDER BY table_name
`

	rows, err := c.db.QueryContext(ctx, query, c.database)
	if err != nil {
		return nil, fmt.Errorf("list source tables: %w", err)
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan source table: %w", err)
		}
		tables = append(tables, Table{Name: name})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source tables: %w", err)
	}

	return tables, nil
}

func (c *SourceClient) ShowCreateTable(ctx context.Context, table string) (string, error) {
	query := fmt.Sprintf("SHOW CREATE TABLE %s", quoteIdentifier(table))

	var name string
	var createSQL string
	if err := c.db.QueryRowContext(ctx, query).Scan(&name, &createSQL); err != nil {
		return "", fmt.Errorf("show create table %q: %w", table, err)
	}

	return createSQL, nil
}

func (c *SourceClient) StreamRows(
	ctx context.Context,
	table string,
	batchSize int,
	notice BatchSizeNotice,
	handle RowBatchHandler,
) error {
	if batchSize <= 0 {
		return fmt.Errorf("stream rows for %q: batch size must be greater than 0", table)
	}

	query := fmt.Sprintf("SELECT * FROM %s", quoteIdentifier(table))
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query rows for %q: %w", table, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read columns for %q: %w", table, err)
	}

	effectiveBatchSize := calculateEffectiveBatchSize(batchSize, len(columns))
	if notice != nil && effectiveBatchSize < batchSize {
		notice(BatchSizeAdjustment{
			Table:          table,
			ConfiguredSize: batchSize,
			EffectiveSize:  effectiveBatchSize,
			ColumnCount:    len(columns),
		})
	}

	batch := RowBatch{
		Columns: append([]string(nil), columns...),
		Rows:    make([][]any, 0, effectiveBatchSize),
	}

	for rows.Next() {
		row, err := scanRow(rows, len(columns))
		if err != nil {
			return fmt.Errorf("scan row for %q: %w", table, err)
		}

		batch.Rows = append(batch.Rows, row)
		if len(batch.Rows) == effectiveBatchSize {
			if err := handle(batch); err != nil {
				return fmt.Errorf("handle row batch for %q: %w", table, err)
			}
			batch.Rows = make([][]any, 0, effectiveBatchSize)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows for %q: %w", table, err)
	}

	if len(batch.Rows) > 0 {
		if err := handle(batch); err != nil {
			return fmt.Errorf("handle row batch for %q: %w", table, err)
		}
	}

	return nil
}

func calculateEffectiveBatchSize(configuredBatchSize, columnCount int) int {
	if configuredBatchSize < 1 {
		configuredBatchSize = 1
	}

	if columnCount < 1 {
		return configuredBatchSize
	}

	maxRowsByColumns := safePlaceholderLimit / columnCount
	if maxRowsByColumns < 1 {
		maxRowsByColumns = 1
	}

	if configuredBatchSize < maxRowsByColumns {
		return configuredBatchSize
	}

	return maxRowsByColumns
}

func buildSourceDSN(cfg config.SourceConfig, tunnelAddr string) (string, error) {
	if strings.TrimSpace(tunnelAddr) == "" {
		return "", fmt.Errorf("source tunnel address is required")
	}

	if _, _, err := net.SplitHostPort(tunnelAddr); err != nil {
		return "", fmt.Errorf("invalid source tunnel address %q: %w", tunnelAddr, err)
	}

	dsn := mysql.Config{
		User:                 cfg.Username,
		Passwd:               cfg.Password,
		Net:                  "tcp",
		Addr:                 tunnelAddr,
		DBName:               cfg.Database,
		AllowNativePasswords: true,
		ParseTime:            true,
	}

	return dsn.FormatDSN(), nil
}

func scanRow(rows rowSet, columnCount int) ([]any, error) {
	values := make([]any, columnCount)
	dest := make([]any, columnCount)

	for i := range values {
		dest[i] = &values[i]
	}

	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}

	for i, value := range values {
		values[i] = cloneValue(value)
	}

	return values, nil
}

func cloneValue(value any) any {
	if bytes, ok := value.([]byte); ok {
		if bytes == nil {
			return nil
		}

		cloned := make([]byte, len(bytes))
		copy(cloned, bytes)
		return cloned
	}
	return value
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func (db sqlDB) QueryContext(ctx context.Context, query string, args ...any) (rowSet, error) {
	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (db sqlDB) QueryRowContext(ctx context.Context, query string, args ...any) scanner {
	return db.DB.QueryRowContext(ctx, query, args...)
}
