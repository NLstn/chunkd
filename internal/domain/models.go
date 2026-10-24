package domain

import "time"

// Server is configuration plus the supervisor's last observed state.
type Server struct {
	ID            uint      `json:"ID" gorm:"primaryKey" odata:"key"`
	Name          string    `json:"Name" gorm:"not null" odata:"required,maxlength=100"`
	State         string    `json:"State" gorm:"not null"`
	CreatedAt     time.Time `json:"CreatedAt"`
	Software      string    `json:"Software"`
	Version       string    `json:"Version"`
	Build         int64     `json:"Build"`
	JavaPath      string    `json:"JavaPath"`
	RequiredJava  int64     `json:"RequiredJava"`
	MemoryMB      int64     `json:"MemoryMB"`
	Port          int64     `json:"Port"`
	EULAAccepted  bool      `json:"EULAAccepted"`
	ArtifactHash  string    `json:"ArtifactHash"`
	HashAlgorithm string    `json:"HashAlgorithm"`
	PID           int64     `json:"PID" gorm:"column:pid"`
	LastError     string    `json:"LastError"`
}

func (Server) TableName() string { return "Servers" }

// Job will represent durable asynchronous operations in later milestones.
type Job struct {
	ID          uint       `json:"ID" gorm:"primaryKey" odata:"key"`
	Kind        string     `json:"Kind" gorm:"not null"`
	State       string     `json:"State" gorm:"not null"`
	CreatedAt   time.Time  `json:"CreatedAt"`
	ServerID    uint       `json:"ServerID"`
	Message     string     `json:"Message"`
	CompletedAt *time.Time `json:"CompletedAt"`
}

func (Job) TableName() string { return "Jobs" }

// Admin and Session are private persistence models, never registered with OData.
type Admin struct {
	ID           uint   `gorm:"primaryKey;check:single_admin,id = 1"`
	Username     string `gorm:"not null;uniqueIndex"`
	PasswordHash string `gorm:"not null"`
}

type Session struct {
	TokenHash string    `gorm:"primaryKey"`
	ExpiresAt time.Time `gorm:"not null;index"`
}
