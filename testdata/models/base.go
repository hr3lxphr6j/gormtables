package models

import "time"

// BaseModel is the shared base struct embedded by most model structs.
type BaseModel struct {
	ID        uint64    `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
