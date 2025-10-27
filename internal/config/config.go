package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 汇总服务运行时所需的全部配置。
type Config struct {
	Addr            string
	AdminUser       string
	AdminPassword   string
	SessionKey      []byte
	CSRFKey         []byte
	DBPath          string
	ScanTimeout     time.Duration
	ScanConcurrency int
}

// Load 从环境变量构建配置，并提供合理的默认值。
func Load() (*Config, error) {
	cfg := &Config{
		Addr:            getenv("PORTNOTE_HTTP_ADDR", ":8080"),
		AdminUser:       getenv("PORTNOTE_ADMIN_USER", "admin"),
		AdminPassword:   getenv("PORTNOTE_ADMIN_PASS", "admin123"),
		SessionKey:      []byte(getenv("PORTNOTE_SESSION_KEY", "0123456789abcdef0123456789abcdef")),
		CSRFKey:         []byte(getenv("PORTNOTE_CSRF_KEY", "abcdef0123456789abcdef0123456789")),
		DBPath:          getenv("PORTNOTE_DB_PATH", "data/portnote.db"),
		ScanTimeout:     durationEnv("PORTNOTE_SCAN_TIMEOUT", 2*time.Second),
		ScanConcurrency: intEnv("PORTNOTE_SCAN_CONCURRENCY", 50),
	}

	if len(cfg.SessionKey) < 32 {
		return nil, fmt.Errorf("session key must be at least 32 bytes, got %d", len(cfg.SessionKey))
	}
	if len(cfg.CSRFKey) < 32 {
		return nil, fmt.Errorf("csrf key must be at least 32 bytes, got %d", len(cfg.CSRFKey))
	}
	if cfg.AdminUser == "" || cfg.AdminPassword == "" {
		return nil, fmt.Errorf("admin credentials must not be empty")
	}
	if cfg.ScanConcurrency <= 0 {
		return nil, fmt.Errorf("scan concurrency must be positive")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func intEnv(key string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return n
}
