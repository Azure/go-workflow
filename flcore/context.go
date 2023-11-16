package flcore

import "context"

type ctxKey struct{}

func TryFromContext[T Logger](ctx context.Context) (T, bool) {
	v, ok := ctx.Value(ctxKey{}).(T)
	return v, ok
}

func FromContext[T Logger](ctx context.Context) T {
	return ctx.Value(ctxKey{}).(T)
}

func NewContext(parent context.Context, logger Logger) context.Context {
	return context.WithValue(parent, ctxKey{}, logger)
}
