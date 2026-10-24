package domain

import "time"

// Server is the persisted configuration contract. Process control arrives in M2.
type Server struct {
	ID        uint      `json:"ID" gorm:"primaryKey" odata:"key"`
	Name      string    `json:"Name" gorm:"not null" odata:"required,maxlength=100"`
	State     string    `json:"State" gorm:"not null"`
	CreatedAt time.Time `json:"CreatedAt"`
}

func (Server) TableName() string { return "Servers" }

// Job will represent durable asynchronous operations in later milestones.
type Job struct {
	ID        uint      `json:"ID" gorm:"primaryKey" odata:"key"`
	Kind      string    `json:"Kind" gorm:"not null"`
	State     string    `json:"State" gorm:"not null"`
	CreatedAt time.Time `json:"CreatedAt"`
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
