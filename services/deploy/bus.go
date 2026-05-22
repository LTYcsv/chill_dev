package main

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

type EventBus struct {
	nc *nats.Conn
}

func NewEventBus(url string) (*EventBus, error) {
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
	)
	if err != nil {
		return nil, err
	}
	return &EventBus{nc: nc}, nil
}

func (b *EventBus) Publish(subject string, payload any) error {
	if b.nc == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return b.nc.Publish(subject, data)
}

func (b *EventBus) Subscribe(subject string, handler func(data []byte)) error {
	if b.nc == nil {
		return nil
	}
	_, err := b.nc.Subscribe(subject, func(m *nats.Msg) {
		handler(m.Data)
	})
	return err
}
