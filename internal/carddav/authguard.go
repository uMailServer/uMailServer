package carddav

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Failed-login throttling for HTTP Basic auth (F6350/F6351). Failures are
// tracked per (normalised username, client IP); after authFreeFailures
// consecutive failures the pair is locked out for an exponentially growing
// window. A success clears the entry.
const (
	authFreeFailures    = 5
	authBaseLockout     = 30 * time.Second
	authMaxLockout      = 15 * time.Minute
	authFailureTTL      = 15 * time.Minute
	maxAuthGuardEntries = 10000
)

type authEntry struct {
	failures int
	last     time.Time
	until    time.Time
}

type authGuard struct {
	mu  sync.Mutex
	m   map[string]*authEntry
	now func() time.Time
}

func newAuthGuard() *authGuard {
	return &authGuard{m: make(map[string]*authEntry), now: time.Now}
}

func authKey(user, ip string) string {
	return strings.ToLower(strings.TrimSpace(user)) + "\x00" + ip
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// locked reports the remaining lockout, if any.
func (g *authGuard) locked(user, ip string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.m[authKey(user, ip)]
	if e == nil {
		return 0
	}
	now := g.now()
	if now.Before(e.until) {
		return e.until.Sub(now)
	}
	if now.Sub(e.last) > authFailureTTL {
		delete(g.m, authKey(user, ip))
	}
	return 0
}

func (g *authGuard) fail(user, ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	k := authKey(user, ip)
	e := g.m[k]
	if e == nil {
		if len(g.m) >= maxAuthGuardEntries {
			g.evict(now)
		}
		e = &authEntry{}
		g.m[k] = e
	} else if now.Sub(e.last) > authFailureTTL {
		e.failures = 0
	}
	e.failures++
	e.last = now
	if e.failures > authFreeFailures {
		d := authBaseLockout
		for i := authFreeFailures + 1; i < e.failures && d < authMaxLockout; i++ {
			d *= 2
		}
		if d > authMaxLockout {
			d = authMaxLockout
		}
		e.until = now.Add(d)
	}
}

// evict drops expired entries, then the stalest one if still full.
func (g *authGuard) evict(now time.Time) {
	var oldestK string
	var oldest time.Time
	for k, e := range g.m {
		if now.After(e.until) && now.Sub(e.last) > authFailureTTL {
			delete(g.m, k)
			continue
		}
		if oldestK == "" || e.last.Before(oldest) {
			oldestK, oldest = k, e.last
		}
	}
	if len(g.m) >= maxAuthGuardEntries && oldestK != "" {
		delete(g.m, oldestK)
	}
}

func (g *authGuard) success(user, ip string) {
	g.mu.Lock()
	delete(g.m, authKey(user, ip))
	g.mu.Unlock()
}

func retryAfterSeconds(d time.Duration) string {
	return strconv.Itoa(int((d + time.Second - 1) / time.Second))
}
