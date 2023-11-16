package to

func Clone[T any](origin *T, fns ...func(*T)) *T {
	rv := *origin
	for _, fn := range fns {
		if fn != nil {
			fn(&rv)
		}
	}
	return &rv
}
