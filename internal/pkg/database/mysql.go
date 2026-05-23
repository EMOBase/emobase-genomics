package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"time"

	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	mysql "github.com/go-sql-driver/mysql"
)

func NewMySQL(cfg configs.MySQLConfig) (*sql.DB, error) {
	dsn := (&mysql.Config{
		User:      cfg.User,
		Passwd:    cfg.Password,
		Net:       "tcp",
		Addr:      fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		DBName:    cfg.Database,
		ParseTime: true,
		Loc:       time.UTC,
	}).FormatDSN()

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
	userInfo := url.UserPassword(cfg.User, cfg.Password)
	return fmt.Sprintf(
		"mysql://%s@tcp(%s:%d)/%s?multiStatements=true",
		userInfo, cfg.Host, cfg.Port, cfg.Database,
	)
}
