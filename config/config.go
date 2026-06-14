package config

import (
	"os"
	"strconv"
)

type Config struct {
	ServerPort               string
	DBHost                   string
	DBPort                   string
	DBName                   string
	DBUser                   string
	DBPassword               string
	RedisHost                string
	RedisPort                string
	VelocityMaxTxnPerMinute  int
	AmountHighValueThreshold float64
	RiskBlockThreshold       int
	RiskReviewThreshold      int
}

func Load() Config {
	return Config{
		ServerPort:               getEnv("SERVER_PORT", "8082"),
		DBHost:                   getEnv("DB_HOST", "localhost"),
		DBPort:                   getEnv("DB_PORT", "5432"),
		DBName:                   getEnv("DB_NAME", "shield_db"),
		DBUser:                   getEnv("DB_USER", "shield"),
		DBPassword:               getEnv("DB_PASSWORD", "shield_pass"),
		RedisHost:                getEnv("REDIS_HOST", "localhost"),
		RedisPort:                getEnv("REDIS_PORT", "6379"),
		VelocityMaxTxnPerMinute:  getEnvInt("VELOCITY_MAX_TXN_PER_MINUTE", 5),
		AmountHighValueThreshold: getEnvFloat("AMOUNT_HIGH_VALUE_THRESHOLD", 50000),
		RiskBlockThreshold:       getEnvInt("RISK_BLOCK_THRESHOLD", 40),
		RiskReviewThreshold:      getEnvInt("RISK_REVIEW_THRESHOLD", 30),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
