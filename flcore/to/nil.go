package to

// IsNilOrEmpty checks whether the pointer is nil or the value is empty.
func IsNilOrEmpty[T comparable](v *T) bool {
	return v == nil || *v == *new(T)
}

func IfEmpty[T comparable](p **T) *ifEmpty[T] { return &ifEmpty[T]{p} }

type ifEmpty[T comparable] struct{ p **T }

func (i *ifEmpty[T]) SetTo(v *T) bool {
	if i.p != nil && *i.p != nil && **i.p == *new(T) {
		*i.p = v
		return true
	}
	return false
}

func IfNil[T any](p **T) *ifNil[T] { return &ifNil[T]{p} }

type ifNil[T any] struct{ p **T }

func (i *ifNil[T]) New() bool {
	if i.p != nil && *i.p == nil {
		*i.p = new(T)
		return true
	}
	return false
}

func (i *ifNil[T]) SetTo(v *T) bool {
	if i.p != nil && *i.p == nil {
		*i.p = v
		return true
	}
	return false
}

func IfNilOrEmpty[T comparable](p **T) *ifNilOrEmpty[T] { return &ifNilOrEmpty[T]{p} }

type ifNilOrEmpty[T comparable] struct{ p **T }

func (i *ifNilOrEmpty[T]) SetTo(v *T) bool {
	if i.p != nil && IsNilOrEmpty(*i.p) {
		*i.p = v
		return true
	}
	return false
}
