package database

import (
	"context"
	"fmt"
	"net"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"minikvx-agent/internal/config"
	"minikvx-agent/internal/domain"
)

func OpenMySQL(cfg config.DatabaseConfig) (*gorm.DB, error) {
	dsnConfig := mysqlDriver.Config{
		User:         cfg.User,
		Passwd:       cfg.Password,
		Net:          "tcp",
		Addr:         net.JoinHostPort(cfg.Host, cfg.Port),
		DBName:       cfg.Name,
		ParseTime:    true,
		Loc:          time.Local,
		Timeout:      3 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		Params: map[string]string{
			"charset": "utf8mb4",
		},
	}
	dsn := dsnConfig.FormatDSN()

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get mysql connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return db, nil
}

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&domain.Task{},
		&domain.TaskStep{},
		&domain.ExecutionLog{},
	); err != nil {
		return fmt.Errorf("migrate mysql schema: %w", err)
	}
	return nil
}
