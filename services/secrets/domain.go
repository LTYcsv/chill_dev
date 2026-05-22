package main

import "time"

type Secret struct {
	ID             string    `json:"id"`
	ServiceID      string    `json:"service_id"`
	EnvironmentID  string    `json:"environment_id"`
	Key            string    `json:"key"`
	ValueEncrypted string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
