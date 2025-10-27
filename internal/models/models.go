package models

import "time"

// User 表示已认证的账户信息。
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Host 表示被追踪端口的目标主机。
type Host struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	AutoScan  bool      `json:"autoScan"`
	Scanning  bool      `json:"scanning"`
	OpenCount int       `json:"openCount"`
	HiddenCount int     `json:"hiddenCount"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Port 用于存储单个端口的元数据。
type Port struct {
	ID          int64     `json:"id"`
	HostID      int64     `json:"hostId"`
	Number      int       `json:"number"`
	Note        string    `json:"note"`
	Fingerprint string    `json:"fingerprint"`
	Hidden      bool      `json:"hidden"`
	Status      string    `json:"status"`
	LastChecked time.Time `json:"lastChecked"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PortStatus 定义端口状态枚举。
const (
	PortStatusUnknown = "unknown"
	PortStatusOpen    = "open"
	PortStatusClosed  = "closed"
)
