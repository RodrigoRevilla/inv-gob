package config

import (
	"fmt"
	"os"
)

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBUrl      string
	JWTSecret  string
	Port       string
}

func Load() *Config {
	cfg := &Config{
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "inv_admin"),
		DBPassword: getEnv("DB_PASSWORD", "cambia_esto_en_produccion"),
		DBName:     getEnv("DB_NAME", "inventario_gov"),
		JWTSecret:  getEnv("JWT_SECRET", "dev_secret_cambia_en_produccion"),
		Port:       getEnv("PORT", "9090"),
	}

	cfg.DBUrl = fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName,
	)

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
