package instance

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

type Redis interface {
	Ping(ctx context.Context) error
	Subscribe(ctx context.Context, ch chan string, subscribeTo ...string)
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) (int, error)
	Set(ctx context.Context, key string, value string) error
	SetEX(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Pipeline(ctx context.Context) redis.Pipeliner
	Exists(ctx context.Context, key string) (bool, error)
	SAdd(ctx context.Context, key string, value string) error
	TTL(ctx context.Context, key string) (time.Duration, error)
}
