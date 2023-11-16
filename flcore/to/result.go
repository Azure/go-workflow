package to

import "strings"

type Result[T any] struct {
	Value T
	Err   error
}

func ResultOf[T any](value T, err error) Result[T] {
	return Result[T]{Value: value, Err: err}
}

func Lower[T ~string](v T) string {
	return strings.ToLower(string(v))
}
