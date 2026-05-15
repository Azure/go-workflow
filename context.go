package flow

import "context"

// ContextKey is the typed-key helper for flowing values through a
// context.Context across go-workflow and its users. Declare a package-level
// variable of ContextKey[T] per value type and use With / From / FromOr at
// the call site:
//
//	type Identity struct{ TenantID, SubID string }
//	var IdentityKey = flow.ContextKey[Identity]{}
//
//	// Caller injects:
//	ctx = IdentityKey.With(ctx, Identity{TenantID: "t", SubID: "s"})
//
//	// Steper reads:
//	func (s *MyStep) Do(ctx context.Context) error {
//	    id, ok := IdentityKey.From(ctx)
//	    if !ok {
//	        return errors.New("identity required")
//	    }
//	    // ... use id ...
//	    return nil
//	}
//
// Uniqueness is by T alone (ContextKey[T] is a zero-size struct used as
// the underlying context key), so two ContextKey[Identity]{} variables
// declared in different packages will collide. Each value type should
// have exactly one canonical key variable, exported by the package that
// owns the type.
type ContextKey[T any] struct{}

// With returns a new context carrying v under k.
func (k ContextKey[T]) With(ctx context.Context, v T) context.Context {
	return context.WithValue(ctx, k, v)
}

// From returns the value stored under k and whether it was present.
func (k ContextKey[T]) From(ctx context.Context) (T, bool) {
	v, ok := ctx.Value(k).(T)
	return v, ok
}

// FromOr returns the value stored under k, or def if no value is present.
func (k ContextKey[T]) FromOr(ctx context.Context, def T) T {
	if v, ok := k.From(ctx); ok {
		return v
	}
	return def
}
