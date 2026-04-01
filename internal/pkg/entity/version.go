package entity

import "time"

type Version struct {
	ID        uint64    `db:"id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
	CreatedBy string    `db:"created_by"`
	UpdatedAt time.Time `db:"updated_at"`
	UpdatedBy string    `db:"updated_by"`
}
