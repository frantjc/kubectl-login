package kubectllogin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Provider interface {
	Available(context.Context) (bool, error)
	Token(context.Context, string) (*Token, error)
}

var (
	providerMux = map[string]Provider{}
	providerMu  sync.Mutex
)

func RegisterProvider(p Provider, scheme string, schemes ...string) {
	providerMu.Lock()
	defer providerMu.Unlock()

	for _, s := range append(schemes, scheme) {
		if _, ok := providerMux[s]; ok {
			panic("attempt to reregister scheme: " + s)
		}

		providerMux[s] = p
	}
}

func NewToken(ctx context.Context, audience string, cache Cache) (*Token, time.Time, error) {
	var errs []error

	for scheme, provider := range providerMux {
		if ce, ok, err := cache.Load(scheme, audience); err != nil {
			errs = append(errs, err)
		} else if ok && time.Now().Before(ce.Exp) {
			return ce.Token, ce.Exp, nil
		}

		if available, err := provider.Available(ctx); available {
			if token, err := provider.Token(ctx, audience); err != nil {
				errs = append(errs, err)
			} else {
				claims, ok := token.Claims.(jwt.MapClaims)
				if !ok {
					return nil, time.Time{}, fmt.Errorf("get token claims")
				}

				exp, ok := claims["exp"].(float64)
				if !ok {
					return nil, time.Time{}, fmt.Errorf("get token exp")
				}

				expirationTimestamp := time.Unix(int64(exp), 0)

				_ = cache.Store(scheme, audience, &CacheEntry{
					Token: token,
					Exp:   expirationTimestamp,
				})

				return token, expirationTimestamp, nil
			}
		} else {
			errs = append(errs, err)
		}
	}

	return nil, time.Time{}, errors.Join(errs...)
}
