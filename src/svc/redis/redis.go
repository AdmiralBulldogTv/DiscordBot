package redis

import (
	"context"
	"sync"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/instance"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

const RedisPrefix = "discord-bot:"

type RedisInst struct {
	client  *redis.Client
	sub     *redis.PubSub
	subsMtx sync.Mutex
	subs    map[string][]*redisSub
}

type SetupOptions struct {
	Username   string
	Password   string
	MasterName string
	Database   int

	Addresses []string
	Sentinel  bool
}

func New(ctx context.Context, opts SetupOptions) (instance.Redis, error) {
	if len(opts.Addresses) == 0 {
		logrus.Fatal("you must provide at least one redis address")
	}

	var rc *redis.Client
	if opts.Sentinel {
		rc = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       opts.MasterName,
			SentinelAddrs:    opts.Addresses,
			SentinelUsername: opts.Username,
			SentinelPassword: opts.Password,
			Username:         opts.Username,
			Password:         opts.Password,
			DB:               opts.Database,
		})
	} else {
		rc = redis.NewClient(&redis.Options{
			Addr:     opts.Addresses[0],
			Username: opts.Username,
			Password: opts.Password,
			DB:       opts.Database,
		})
	}

	if err := rc.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	inst := &RedisInst{
		client: rc,
		sub:    rc.Subscribe(context.Background()),
		subs:   map[string][]*redisSub{},
	}
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logrus.WithField("err", err).Fatal("panic in subs")
			}
		}()
		ch := inst.sub.Channel()
		var msg *redis.Message
		for {
			msg = <-ch
			payload := msg.Payload // dont change we want to copy the memory due to concurrency.
			inst.subsMtx.Lock()
			for _, s := range inst.subs[msg.Channel] {
				select {
				case s.ch <- payload:
				default:
					logrus.Warn("channel blocked dropping message: ", msg.Channel)
				}
			}
			inst.subsMtx.Unlock()
		}
	}()

	return inst, nil
}

type redisSub struct {
	ch chan string
}

// Subscribe to a channel on Redis
func (r *RedisInst) Subscribe(ctx context.Context, ch chan string, subscribeTo ...string) {
	r.subsMtx.Lock()
	defer r.subsMtx.Unlock()
	localSub := &redisSub{ch}
	for _, e := range subscribeTo {
		if _, ok := r.subs[e]; !ok {
			_ = r.sub.Subscribe(ctx, e)
		}
		r.subs[e] = append(r.subs[e], localSub)
	}

	go func() {
		<-ctx.Done()
		r.subsMtx.Lock()
		defer r.subsMtx.Unlock()
		for _, e := range subscribeTo {
			for i, v := range r.subs[e] {
				if v == localSub {
					if i != len(r.subs[e])-1 {
						r.subs[e][i] = r.subs[e][len(r.subs[e])-1]
					}
					r.subs[e] = r.subs[e][:len(r.subs[e])-1]
					if len(r.subs[e]) == 0 {
						delete(r.subs, e)
						if err := r.sub.Unsubscribe(context.Background(), e); err != nil {
							logrus.WithError(err).Error("failed to unsubscribe")
						}
					}
					break
				}
			}
		}
	}()
}

func (r *RedisInst) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisInst) Del(ctx context.Context, key string) (int, error) {
	i, err := r.client.Del(ctx).Result()
	return int(i), err
}
func (r *RedisInst) Exists(ctx context.Context, key string) (bool, error) {
	i, err := r.client.Exists(ctx, key).Result()
	return i > 0, err
}

func (r *RedisInst) SetEX(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return r.client.SetEX(ctx, key, value, ttl).Err()
}

func (r *RedisInst) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, key, value, ttl).Result()
}

func (r *RedisInst) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *RedisInst) Set(ctx context.Context, key string, value string) error {
	return r.client.Set(ctx, key, value, 0).Err()
}

func (r *RedisInst) SAdd(ctx context.Context, key string, value string) error {
	return r.client.SAdd(ctx, key, value).Err()
}

func (r *RedisInst) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

func (r *RedisInst) Pipeline(ctx context.Context) redis.Pipeliner {
	return r.client.Pipeline()
}

func (r *RedisInst) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}
