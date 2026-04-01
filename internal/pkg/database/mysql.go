package database

import (
	"database/sql"
	"fmt"
	"time"

	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	_ "github.com/go-sql-driver/mysql"
)

func NewMySQL(cfg configs.MySQLConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=UTC",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("mysql ping failed: %w", err)
	}

	return db, nil
}

func MySQLMigrateDSN(cfg configs.MySQLConfig) string {
	return fmt.Sprintf(
		"mysql://%s:%s@tcp(%s:%d)/%s?multiStatements=true",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
	)
}
