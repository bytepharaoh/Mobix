package authn

import (
	"context"
	"net/http"
	"strings"
)

type Role string

const (
	Rider    Role = "rider"
	Driver   Role = "driver"
	Operator Role = "operator"
	Service  Role = "service"
)

type Actor struct {
	ID   string `json:"id"`
	Role Role   `json:"role"`
}

type actorKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func From(ctx context.Context) Actor {
	actor, _ := ctx.Value(actorKey{}).(Actor)
	return actor
}

func Bearer(r *http.Request) string {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		return ""
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.ContainsAny(token, " \t,") {
		return ""
	}
	return token
}
