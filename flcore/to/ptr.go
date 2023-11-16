package to

func Ptr[T any](v T) *T {
	return &v
}

func Deref[T any](v *T) T {
	if v == nil {
		return *new(T)
	}
	return *v
}
