package to

// Coalesce returns the first non-nil / non-empty value.
func Coalesce[T comparable](vs ...T) T {
	var empty T
	for _, v := range vs {
		if v != empty {
			return v
		}
	}
	return empty
}

// CoalesceFunc executes the callbacks and returns the first non-nil / non-empty result.
func CoalesceFunc[T comparable](fns ...func() T) T {
	var empty T
	for _, fn := range fns {
		if fn != nil {
			if v := fn(); v != empty {
				return v
			}
		}
	}
	return empty
}

type OverrideFunc[T any] func(T) T

// Override chains and applies override functions.
func Override[T any](fns ...OverrideFunc[T]) OverrideFunc[T] {
	return func(t T) T {
		for _, fn := range fns {
			if fn != nil {
				t = fn(t)
			}
		}
		return t
	}
}
