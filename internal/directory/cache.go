package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	kubectllogin "github.com/frantjc/kubectl-login"
	xerrors "github.com/frantjc/x/errors"
)

func Register() {
	kubectllogin.RegisterCache(
		kubectllogin.OpenerFunc[kubectllogin.Cache](func(ctx context.Context, u *url.URL) (kubectllogin.Cache, error) {
			path := filepath.Join(u.Host, u.Path)
			if strings.HasPrefix(path, "~") {
				home := os.Getenv("HOME")
				if home == "" {
					if u, err := user.Current(); err == nil {
						home = u.HomeDir
					}
				}
				path = home + path[1:]
			}
			return Cache(path), nil
		}),
		"file",
	)
}

type Cache string

var (
	_ kubectllogin.Cache = Cache("")
)

func (c Cache) String() string {
	return string(c)
}

// Load implements [Cache].
func (c Cache) Load(provider, audience string) (*kubectllogin.CacheEntry, bool, error) {
	rawCacheEntry, err := os.ReadFile(filepath.Join(c.String(), fmt.Sprintf("%s-%s.json", provider, audience)))
	if err != nil {
		return nil, false, xerrors.Ignore(err, os.ErrNotExist)
	}
	ce := &kubectllogin.CacheEntry{}
	if err := json.Unmarshal(rawCacheEntry, ce); err != nil {
		return nil, false, nil
	}
	return ce, true, nil
}

// Store implements [Cache].
func (c Cache) Store(provider, audience string, ce *kubectllogin.CacheEntry) error {
	rawCacheEntry, err := json.Marshal(ce)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.String(), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.String(), fmt.Sprintf("%s-%s.json", provider, audience)), rawCacheEntry, 0600)
}
