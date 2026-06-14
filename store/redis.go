package store

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
	"github.com/upi/shield/config"
)

func NewRedis(cfg config.Config) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("[SHIELD] redis ping error: %v", err)
	}

	log.Println("[SHIELD] Redis connected")
	return client
}
