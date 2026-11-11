package config

import (
	"net"
	"net/url"
)

func RabbitURL() string {
	if value := GetString("RABBITMQ_URL", ""); value != "" {
		return value
	}
	uri := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(GetString("RABBITMQ_DEFAULT_USER", "mobix"), GetString("RABBITMQ_DEFAULT_PASS", "mobix-local")),
		Host:   net.JoinHostPort(GetString("RABBITMQ_HOST", "localhost"), GetString("RABBITMQ_PORT", "5672")),
		Path:   "/",
	}
	return uri.String()
}
