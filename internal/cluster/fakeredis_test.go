package cluster

// fakeRedis is an in-memory Redis model installed as
// a go-redis ProcessHook that never calls next, so no network is touched. It
// models only the commands this package issues: GET, SET..NX, EXPIRE,
// PEXPIRE, EVALSHA (always NOSCRIPT, forcing EVAL) and EVAL of the
// compare-then-act scripts used by the package.

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
)

type fakeRedis struct {
	mu   sync.Mutex
	vals map[string]string
	ttl  map[string]int64 // milliseconds; absent = no expiry
	log  []string
	// beforeCmd, if set, runs (outside mu) before command number n (1-based)
	// of every command processed. It models another cluster instance acting
	// between two of our round trips.
	beforeCmd func(n int)
	n         int
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{vals: map[string]string{}, ttl: map[string]int64{}}
}

func (f *fakeRedis) client() *redis.Client {
	c := redis.NewClient(&redis.Options{Addr: "audit.invalid:0"})
	c.AddHook(f)
	return c
}

// Put sets key directly (another instance's write).
func (f *fakeRedis) Put(key, val string, ttlMs int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vals[key] = val
	f.ttl[key] = ttlMs
}

func (f *fakeRedis) State(key string) (string, int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[key]
	return v, f.ttl[key], ok
}

func (f *fakeRedis) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, n, a string) (net.Conn, error) { panic("fake redis: unexpected dial") }
}

func (f *fakeRedis) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error { panic("fake redis: unexpected pipeline") }
}

func fakeStr(v interface{}) string { return fmt.Sprint(v) }

func (f *fakeRedis) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		f.mu.Lock()
		f.n++
		n := f.n
		hook := f.beforeCmd
		f.mu.Unlock()
		if hook != nil {
			hook(n)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		args := cmd.Args()
		name := strings.ToLower(fakeStr(args[0]))
		f.log = append(f.log, name)
		switch name {
		case "get":
			c := cmd.(*redis.StringCmd)
			v, ok := f.vals[fakeStr(args[1])]
			if !ok {
				c.SetErr(redis.Nil)
				return redis.Nil
			}
			c.SetVal(v)
			return nil
		case "set":
			c := cmd.(*redis.BoolCmd)
			key, val := fakeStr(args[1]), fakeStr(args[2])
			if _, ok := f.vals[key]; ok {
				c.SetVal(false)
				return nil
			}
			var ms int64
			for i := 3; i+1 < len(args); i++ {
				x, _ := strconv.ParseInt(fakeStr(args[i+1]), 10, 64)
				switch strings.ToLower(fakeStr(args[i])) {
				case "px":
					ms = x
				case "ex":
					ms = x * 1000
				}
			}
			f.vals[key], f.ttl[key] = val, ms
			c.SetVal(true)
			return nil
		case "expire", "pexpire":
			c := cmd.(*redis.BoolCmd)
			key := fakeStr(args[1])
			x, _ := strconv.ParseInt(fakeStr(args[2]), 10, 64)
			c.SetVal(f.expireLocked(key, name, x) == 1)
			return nil
		case "evalsha":
			cmd.SetErr(redis.ErrNoScript)
			return redis.ErrNoScript
		case "eval":
			c := cmd.(*redis.Cmd)
			script := fakeStr(args[1])
			key := fakeStr(args[3])
			argv := args[4:]
			if f.vals[key] != fakeStr(argv[0]) {
				c.SetVal(int64(0))
				return nil
			}
			var res int64
			switch {
			case strings.Contains(script, `"DEL"`):
				delete(f.vals, key)
				delete(f.ttl, key)
				res = 1
			case strings.Contains(script, `"PEXPIRE"`):
				x, _ := strconv.ParseInt(fakeStr(argv[1]), 10, 64)
				res = f.expireLocked(key, "pexpire", x)
			case strings.Contains(script, `"EXPIRE"`):
				x, _ := strconv.ParseInt(fakeStr(argv[1]), 10, 64)
				res = f.expireLocked(key, "expire", x)
			default:
				panic("fake redis: unknown script")
			}
			c.SetVal(res)
			return nil
		}
		panic("fake redis: unexpected command " + name)
	}
}

// expireLocked mirrors Redis: a non-positive timeout deletes the key.
func (f *fakeRedis) expireLocked(key, name string, x int64) int64 {
	if _, ok := f.vals[key]; !ok {
		return 0
	}
	if name == "expire" {
		x *= 1000
	}
	if x <= 0 {
		delete(f.vals, key)
		delete(f.ttl, key)
		return 1
	}
	f.ttl[key] = x
	return 1
}
