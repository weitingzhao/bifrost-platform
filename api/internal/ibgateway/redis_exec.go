package ibgateway

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// These reads used to run `kubectl exec -n data deploy/redis-ib -- redis-cli`.
// The platform-api image carries neither kubectl nor redis-cli, so every one of
// them failed in the cluster and the ib-gateway probe reported "redis-ib probe
// failed" permanently — it only ever worked on a developer's machine, where
// kubectl happens to be on PATH. It was also the one shell-out left in this
// codebase; everything else reaches the cluster through client-go.
//
// redis-ib is an ordinary ClusterIP in the data namespace and its NetworkPolicy
// already admits bifrost-platform-{prod,stg} on 6379, so platform-api can dial
// it directly with the ACL user it already has a password for.

const redisOpTimeout = 3 * time.Second

// A handful of slots is the real shape; the cap only stops a probe from
// walking an unexpectedly large keyspace.
const redisScanMaxKeys = 64

var errRedisPassMissing = errors.New("REDIS_IB_PLATFORM_PASS not set")

// redisClient is built once and reused; go-redis pools connections internally.
func (s *Service) redisClient() (*redis.Client, error) {
	if s.cfg.RedisPlatformPass == "" {
		return nil, errRedisPassMissing
	}
	addr := s.cfg.RedisAddr
	if addr == "" {
		// A Service built without ConfigFromEnv still gets the cluster address.
		addr = redisServiceHost
	}
	s.redisOnce.Do(func() {
		s.redis = redis.NewClient(&redis.Options{
			Addr:         addr,
			Username:     redisACLUser,
			Password:     s.cfg.RedisPlatformPass,
			DialTimeout:  redisOpTimeout,
			ReadTimeout:  redisOpTimeout,
			WriteTimeout: redisOpTimeout,
		})
	})
	return s.redis, nil
}

func (s *Service) redisCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisOpTimeout)
}

func (s *Service) redisHGetAll(key string) (map[string]string, error) {
	c, err := s.redisClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.redisCtx()
	defer cancel()
	return c.HGetAll(ctx, key).Result()
}

// redisGet returns "" for a missing key — callers treat absent and empty alike.
func (s *Service) redisGet(key string) (string, error) {
	c, err := s.redisClient()
	if err != nil {
		return "", err
	}
	ctx, cancel := s.redisCtx()
	defer cancel()
	v, err := c.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

// redisScanKeys walks the keyspace with SCAN rather than KEYS so a growing
// keyspace cannot stall the probe. The ACL user is scoped to the patterns it is
// allowed to see, and Redis filters the cursor to those.
func (s *Service) redisScanKeys(pattern string) ([]string, error) {
	c, err := s.redisClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.redisCtx()
	defer cancel()

	var keys []string
	iter := c.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= redisScanMaxKeys {
			break
		}
	}
	return keys, iter.Err()
}

func (s *Service) redisSet(key, value string, ttl time.Duration) error {
	c, err := s.redisClient()
	if err != nil {
		return err
	}
	ctx, cancel := s.redisCtx()
	defer cancel()
	return c.Set(ctx, key, value, ttl).Err()
}

func (s *Service) redisHSet(key string, values ...any) error {
	c, err := s.redisClient()
	if err != nil {
		return err
	}
	ctx, cancel := s.redisCtx()
	defer cancel()
	return c.HSet(ctx, key, values...).Err()
}

// redisCLI is the last caller of the retired shell-out transport, and it is the
// one path that must stay shut: the operator command stream has a single
// legitimate writer (the daemon itself), named in spine decision D10, which is
// BLOCKED. That write already failed in every cluster — the old transport
// needed a kubectl this image does not carry — so closing it explicitly changes
// no behaviour, it only replaces an accidental failure with a deliberate one.
// Reconnect already falls through to a rollout restart when this returns an
// error, which is what it has always done in practice.
//
// When D10 unlocks, this becomes an XAdd through the same client as the reads.
func (s *Service) redisCLI(args ...string) (string, error) {
	_ = args
	return "", errOperatorWriteBlocked
}

var errOperatorWriteBlocked = errors.New(
	"platform-api does not write the operator command stream (spine D10 BLOCKED; the daemon is its only writer)")
