// Package env — чтение настроек из окружения. Других источников у сервисов
// нет: в k3s всё задаётся в манифесте.
package env

import (
	"os"
	"strconv"
	"time"
)

func String(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func Int(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func Duration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func Bool(key string) bool {
	v, _ := strconv.ParseBool(os.Getenv(key))
	return v
}
