package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hitushen/portnotepro/internal/models"
	"github.com/hitushen/portnotepro/internal/targets"
	"golang.org/x/crypto/bcrypt"
)

// Store 封装了对 SQLite 数据库的持久化访问。
type Store struct {
	DB *sql.DB
}

// PortQuery 用于列举端口时提供可选过滤条件。
type PortQuery struct {
	Search   string
	Status   string
	SortBy   string
	SortDesc bool
	Page     int
	PageSize int
}

// New 根据给定的 SQLite 文件路径初始化 Store。
func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 更适合单写入，这里保持简单配置。

	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close 释放数据库资源。
func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) migrate() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS hosts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			address TEXT NOT NULL,
			auto_scan INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_hosts_name ON hosts(name);`,
		`CREATE TABLE IF NOT EXISTS ports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
			number INTEGER NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			fingerprint TEXT NOT NULL DEFAULT '',
			hidden INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'unknown',
			last_checked TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(host_id, number)
		);`,
	}
	for _, stmt := range schema {
		if _, err := s.DB.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if err := s.ensureHostColumns(); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureHostColumns() error {
	rows, err := s.DB.Query(`PRAGMA table_info(hosts)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, "scanning") {
			return nil
		}
	}

	_, err = s.DB.Exec(`ALTER TABLE hosts ADD COLUMN scanning INTEGER NOT NULL DEFAULT 0`)
	return err
}

// EnsureAdmin 根据给定凭证创建或更新管理员账号，确保其存在。
func (s *Store) EnsureAdmin(ctx context.Context, username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existingID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&existingID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, string(hash)); err != nil {
			return fmt.Errorf("create admin: %w", err)
		}
	} else if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, created_at = CURRENT_TIMESTAMP WHERE id = ?`, string(hash), existingID); err != nil {
			return fmt.Errorf("update admin: %w", err)
		}
	} else {
		return err
	}

	return tx.Commit()
}

// Authenticate 校验登录凭证，成功时返回用户 ID。
func (s *Store) Authenticate(ctx context.Context, username, password string) (*models.User, error) {
	var user models.User
	err := s.DB.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at FROM users WHERE username = ?`, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid credentials")
		}
		return nil, err
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, errors.New("invalid credentials")
	}
	return &user, nil
}

// ListHosts 按名称排序返回全部主机记录。
func (s *Store) ListHosts(ctx context.Context) ([]models.Host, error) {
rows, err := s.DB.QueryContext(ctx, `
        SELECT 
            h.id, h.name, h.address, h.auto_scan, h.scanning, h.created_at, h.updated_at,
            (SELECT COUNT(1) FROM ports p WHERE p.host_id = h.id AND p.hidden = 0) AS open_count,
            (SELECT COUNT(1) FROM ports p WHERE p.host_id = h.id AND p.hidden = 1) AS hidden_count
        FROM hosts h
        ORDER BY h.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []models.Host
	for rows.Next() {
		var h models.Host
		var autoScan, scanning int
		if err := rows.Scan(&h.ID, &h.Name, &h.Address, &autoScan, &scanning, &h.CreatedAt, &h.UpdatedAt, &h.OpenCount, &h.HiddenCount); err != nil {
			return nil, err
		}
		h.Address = targets.Normalize(h.Address)
		h.AutoScan = autoScan == 1
		h.Scanning = scanning == 1
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// GetHost 根据 ID 获取主机信息。
func (s *Store) GetHost(ctx context.Context, id int64) (*models.Host, error) {
	var h models.Host
	var autoScan, scanning int
err := s.DB.QueryRowContext(ctx, `
        SELECT 
            id, name, address, auto_scan, scanning, created_at, updated_at,
            (SELECT COUNT(1) FROM ports p WHERE p.host_id = hosts.id AND p.hidden = 0) AS open_count,
            (SELECT COUNT(1) FROM ports p WHERE p.host_id = hosts.id AND p.hidden = 1) AS hidden_count
        FROM hosts WHERE id = ?`, id).
        Scan(&h.ID, &h.Name, &h.Address, &autoScan, &scanning, &h.CreatedAt, &h.UpdatedAt, &h.OpenCount, &h.HiddenCount)
	if err != nil {
		return nil, err
	}
	h.Address = targets.Normalize(h.Address)
	h.AutoScan = autoScan == 1
	h.Scanning = scanning == 1
	return &h, nil
}

// CreateHost 创建新的主机记录。
func (s *Store) CreateHost(ctx context.Context, name, address string, autoScan bool) (int64, error) {
    res, err := s.DB.ExecContext(ctx, `INSERT INTO hosts (name, address, auto_scan, scanning) VALUES (?, ?, ?, 0)`, name, address, boolToInt(autoScan))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateHost 更新主机字段。
func (s *Store) UpdateHost(ctx context.Context, id int64, name, address string, autoScan bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE hosts SET name = ?, address = ?, auto_scan = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, address, boolToInt(autoScan), id)
	return err
}

// DeleteHost 删除主机及其关联端口。
func (s *Store) DeleteHost(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM hosts WHERE id = ?`, id)
	return err
}

func (s *Store) BeginScan(ctx context.Context, hostID int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE hosts SET scanning = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND scanning = 0`, hostID)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) EndScan(ctx context.Context, hostID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE hosts SET scanning = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, hostID)
	return err
}

// ListPorts 返回指定主机的端口，可选择包含隐藏记录。
func (s *Store) ListPorts(ctx context.Context, hostID int64, includeHidden bool) ([]models.Port, error) {
	ports, _, err := s.ListPortsWithQuery(ctx, hostID, includeHidden, nil)
	return ports, err
}

// ListPortsWithQuery 结合过滤与分页条件返回主机端口。
func (s *Store) ListPortsWithQuery(ctx context.Context, hostID int64, includeHidden bool, q *PortQuery) ([]models.Port, int, error) {
	query := &PortQuery{}
	if q != nil {
		*query = *q
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize < 0 {
		query.PageSize = 0
	}

	base := `FROM ports WHERE host_id = ?`
	args := []interface{}{hostID}

	if !includeHidden {
		base += ` AND hidden = 0`
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		base += ` AND (CAST(number AS TEXT) LIKE ? OR fingerprint LIKE ? OR note LIKE ?)`
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		base += ` AND status = ?`
		args = append(args, status)
	}

	countSQL := `SELECT COUNT(1) ` + base
	var total int
	if err := s.DB.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortColumn := "number"
	switch strings.ToLower(query.SortBy) {
	case "number":
		sortColumn = "number"
	case "last_checked":
		sortColumn = "last_checked"
	case "updated_at":
		sortColumn = "updated_at"
	case "fingerprint":
		sortColumn = "fingerprint"
	default:
		sortColumn = "number"
	}
	orderExpr := sortColumn
	if query.SortDesc {
		orderExpr += " DESC"
	} else {
		orderExpr += " ASC"
	}

	selectSQL := `SELECT id, host_id, number, note, fingerprint, hidden, status, last_checked, created_at, updated_at ` + base + ` ORDER BY ` + orderExpr
	if query.PageSize > 0 {
		offset := (query.Page - 1) * query.PageSize
		selectSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", query.PageSize, offset)
	}

	rows, err := s.DB.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var ports []models.Port
	for rows.Next() {
		var p models.Port
		var hidden int
		var lastChecked sql.NullTime
		if err := rows.Scan(&p.ID, &p.HostID, &p.Number, &p.Note, &p.Fingerprint, &hidden, &p.Status, &lastChecked, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, err
		}
		p.Hidden = hidden == 1
		if lastChecked.Valid {
			p.LastChecked = lastChecked.Time
		}
		ports = append(ports, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return ports, total, nil
}

// CreatePort 新增端口记录。
func (s *Store) CreatePort(ctx context.Context, hostID int64, number int, note, fingerprint string) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO ports (host_id, number, note, fingerprint, status) VALUES (?, ?, ?, ?, ?)`,
		hostID, number, note, fingerprint, models.PortStatusUnknown,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeletePort 删除端口记录。
func (s *Store) DeletePort(ctx context.Context, portID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM ports WHERE id = ?`, portID)
	return err
}

// GetPort 根据端口 ID 查询端口。
func (s *Store) GetPort(ctx context.Context, portID int64) (*models.Port, error) {
	var p models.Port
	var lastChecked sql.NullTime
	var hidden int
	err := s.DB.QueryRowContext(ctx, `SELECT id, host_id, number, note, fingerprint, hidden, status, last_checked, created_at, updated_at FROM ports WHERE id = ?`, portID).
		Scan(&p.ID, &p.HostID, &p.Number, &p.Note, &p.Fingerprint, &hidden, &p.Status, &lastChecked, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if lastChecked.Valid {
		p.LastChecked = lastChecked.Time
	}
	p.Hidden = hidden == 1
	return &p, nil
}

// UpdatePortNote 更新端口备注与指纹。
func (s *Store) UpdatePortNote(ctx context.Context, portID int64, note, fingerprint string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE ports SET note = ?, fingerprint = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		note, fingerprint, portID,
	)
	return err
}

// UpdatePortFingerprint 仅更新指纹信息。
func (s *Store) UpdatePortFingerprint(ctx context.Context, portID int64, fingerprint string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE ports SET fingerprint = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		fingerprint, portID,
	)
	return err
}

// SetPortHidden 设置端口隐藏标记。
func (s *Store) SetPortHidden(ctx context.Context, portID int64, hidden bool) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE ports SET hidden = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		boolToInt(hidden), portID,
	)
	return err
}

// BulkSetHidden 在给定范围内批量修改隐藏标记（包含端点）。
func (s *Store) BulkSetHidden(ctx context.Context, hostID int64, start, end int, hidden bool) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE ports SET hidden = ?, updated_at = CURRENT_TIMESTAMP WHERE host_id = ? AND number BETWEEN ? AND ?`,
		boolToInt(hidden), hostID, start, end,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// BulkDeletePorts 删除指定范围内的端口。
func (s *Store) BulkDeletePorts(ctx context.Context, hostID int64, start, end int) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM ports WHERE host_id = ? AND number BETWEEN ? AND ?`,
		hostID, start, end,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdatePortStatus 写入最新的端口状态检查结果。
func (s *Store) UpdatePortStatus(ctx context.Context, portID int64, status string, t time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE ports SET status = ?, last_checked = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, t.UTC(), portID,
	)
	return err
}

// FindPortByNumber 根据主机与端口号查询端口。
func (s *Store) FindPortByNumber(ctx context.Context, hostID int64, number int) (*models.Port, error) {
	var p models.Port
	var lastChecked sql.NullTime
	var hidden int
	err := s.DB.QueryRowContext(ctx, `SELECT id, host_id, number, note, fingerprint, hidden, status, last_checked, created_at, updated_at FROM ports WHERE host_id = ? AND number = ?`,
		hostID, number).
		Scan(&p.ID, &p.HostID, &p.Number, &p.Note, &p.Fingerprint, &hidden, &p.Status, &lastChecked, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Hidden = hidden == 1
	if lastChecked.Valid {
		p.LastChecked = lastChecked.Time
	}
	return &p, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
