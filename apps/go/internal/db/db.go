package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

const defaultSQLiteURL = "sqlite://../../data/pipelogiq.db"

// Connect opens a sqlx.DB with retry and sane defaults.
func Connect(ctx context.Context, dsn string, logger *slog.Logger) (*sqlx.DB, error) {
	driver, normalizedDSN, err := normalizeDatabaseURL(dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		if err := ensureSQLiteParentDir(normalizedDSN); err != nil {
			return nil, fmt.Errorf("prepare sqlite database path: %w", err)
		}
	}

	var db *sqlx.DB
	operation := func() error {
		db, err = sqlx.Open(driver, normalizedDSN)
		if err != nil {
			return err
		}
		configureConnectionPool(db, driver)

		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			_ = db.Close()
			return err
		}
		return nil
	}

	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 500 * time.Millisecond
	exp.MaxElapsedTime = 2 * time.Minute
	if driver == "sqlite" {
		exp.MaxElapsedTime = 5 * time.Second
	}

	if err := backoff.Retry(operation, backoff.WithContext(exp, ctx)); err != nil {
		return nil, fmt.Errorf("connect to db: %w", err)
	}

	logger.Info("connected to database", "driver", driver)
	return db, nil
}

func configureConnectionPool(db *sqlx.DB, driver string) {
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
		return
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
}

func normalizeDatabaseURL(dsn string) (string, string, error) {
	raw := strings.TrimSpace(dsn)
	if raw == "" {
		raw = defaultSQLiteURL
	}

	switch {
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		return "pgx", raw, nil
	case strings.HasPrefix(raw, "sqlite://"):
		path := strings.TrimPrefix(raw, "sqlite://")
		if strings.TrimSpace(path) == "" {
			return "", "", fmt.Errorf("sqlite database path is required")
		}
		return "sqlite", buildSQLiteDSN(path), nil
	case strings.HasPrefix(raw, "file:"):
		return "sqlite", appendSQLitePragmas(raw), nil
	case strings.HasSuffix(raw, ".db"), strings.HasSuffix(raw, ".sqlite"), strings.HasSuffix(raw, ".sqlite3"):
		return "sqlite", buildSQLiteDSN(raw), nil
	default:
		// Preserve backward compatibility for existing pgx-style DSNs.
		return "pgx", raw, nil
	}
}

func buildSQLiteDSN(path string) string {
	if strings.HasPrefix(path, "file:") {
		return appendSQLitePragmas(path)
	}
	return appendSQLitePragmas("file:" + path)
}

func appendSQLitePragmas(dsn string) string {
	if strings.Contains(dsn, "_pragma=foreign_keys(ON)") || strings.Contains(dsn, "_pragma=foreign_keys(1)") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&_pragma=foreign_keys(ON)"
	}
	return dsn + "?_pragma=foreign_keys(ON)"
}

func ensureSQLiteParentDir(dsn string) error {
	if !strings.HasPrefix(dsn, "file:") {
		return nil
	}

	pathWithQuery := strings.TrimPrefix(dsn, "file:")
	path := pathWithQuery
	if idx := strings.Index(pathWithQuery, "?"); idx >= 0 {
		path = pathWithQuery[:idx]
	}
	path = strings.TrimSpace(path)
	if path == "" || strings.EqualFold(path, ":memory:") {
		return nil
	}

	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}

	return os.MkdirAll(dir, 0o755)
}
