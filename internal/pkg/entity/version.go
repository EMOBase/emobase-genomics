package entity

import "time"

type Version struct {
	ID        uint64    `db:"id"         json:"id"`
	Name      string    `db:"name"       json:"name"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	CreatedBy string    `db:"created_by" json:"createdBy"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
	UpdatedBy string    `db:"updated_by" json:"updatedBy"`
}
