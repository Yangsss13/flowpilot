package database

import (
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yangsss13/flowpilot/internal/config"
)

func OpenRabbitMQ(cfg config.RabbitMQConfig) (*amqp.Connection, error) {
	connection, err := amqp.DialConfig(cfg.URL, amqp.Config{
		Dial:      amqp.DefaultDial(3 * time.Second),
		Heartbeat: 10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("connect RabbitMQ: %w", err)
	}
	return connection, nil
}
