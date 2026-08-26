package kubectllogin

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"
)

type CacheEntry struct {
	Token *Token    `json:"token"`
	Exp   time.Time `json:"exp,omitempty"`
}

type Cache interface {
	Store(string, string, *CacheEntry) error
	Load(string, string) (*CacheEntry, bool, error)
}

var (
	cacheMux = map[string]Opener[Cache]{}
	cacheMu  sync.Mutex
)

func RegisterCache(o Opener[Cache], scheme string, schemes ...string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	for _, s := range append(schemes, scheme) {
		if _, ok := cacheMux[s]; ok {
			panic("attempt to reregister scheme: " + s)
		}

		cacheMux[s] = o
	}
}

func NewCache(ctx context.Context, raw string) (Cache, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	if o, ok := cacheMux[u.Scheme]; ok {
		return o.Open(ctx, u)
	}

	return nil, fmt.Errorf("no cache registered for scheme %s", u.Scheme)
}
