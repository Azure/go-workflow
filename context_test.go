package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextKey(t *testing.T) {
	type creds struct{ Token string }
	var key = ContextKey[creds]{}

	t.Run("From returns ok=false when unset", func(t *testing.T) {
		_, ok := key.From(context.Background())
		assert.False(t, ok)
	})

	t.Run("FromOr returns default when unset", func(t *testing.T) {
		def := creds{Token: "default"}
		got := key.FromOr(context.Background(), def)
		assert.Equal(t, def, got)
	})

	t.Run("With + From round-trips the value", func(t *testing.T) {
		want := creds{Token: "abc"}
		ctx := key.With(context.Background(), want)
		got, ok := key.From(ctx)
		assert.True(t, ok)
		assert.Equal(t, want, got)
	})

	t.Run("FromOr returns stored value over default", func(t *testing.T) {
		want := creds{Token: "abc"}
		ctx := key.With(context.Background(), want)
		got := key.FromOr(ctx, creds{Token: "fallback"})
		assert.Equal(t, want, got)
	})

	t.Run("keys with different T do not collide", func(t *testing.T) {
		var keyA = ContextKey[string]{}
		var keyB = ContextKey[int]{}
		ctx := keyA.With(context.Background(), "hello")
		ctx = keyB.With(ctx, 42)

		gotA, okA := keyA.From(ctx)
		gotB, okB := keyB.From(ctx)
		assert.True(t, okA)
		assert.Equal(t, "hello", gotA)
		assert.True(t, okB)
		assert.Equal(t, 42, gotB)
	})

	t.Run("two distinct ContextKey[T] vars share the same key", func(t *testing.T) {
		// ContextKey[T]{} is a zero-size struct keyed only by T, so two
		// separate package-level vars of the same T will collide. This
		// documents the contract: each value type should have ONE canonical
		// key var (typically exported by the package that owns the type).
		var k1 = ContextKey[string]{}
		var k2 = ContextKey[string]{}
		ctx := k1.With(context.Background(), "x")
		got, ok := k2.From(ctx)
		assert.True(t, ok)
		assert.Equal(t, "x", got)
	})
}
