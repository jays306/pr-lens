package harness

import "context"

type authKey struct{}

type reviewAuth struct {
	token string
	owner string
}

// WithAuth attaches the review GitHub token and repo owner for private modules.
func WithAuth(ctx context.Context, token, owner string) context.Context {
	return context.WithValue(ctx, authKey{}, reviewAuth{token: token, owner: owner})
}

func authFrom(ctx context.Context) (token, owner string) {
	a, _ := ctx.Value(authKey{}).(reviewAuth)
	return a.token, a.owner
}
