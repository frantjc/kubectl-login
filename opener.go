package kubectllogin

import (
	"context"
	"net/url"
)

type Opener[T any] interface {
	Open(context.Context, *url.URL) (T, error)
}

type OpenerFunc[T any] func(context.Context, *url.URL) (T, error)

func (o OpenerFunc[T]) Open(ctx context.Context, u *url.URL) (T, error) {
	return o(ctx, u)
}
