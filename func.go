package flow

import (
	"context"
)

// FuncIO constructs a Step from an arbitrary function
func FuncIO[I, O any](name string, do func(context.Context, I) (O, error)) *Function[I, O] {
	f := &Function[I, O]{Name: name, do: do}
	return f
}
func FuncI[I any](name string, do func(context.Context, I) error) *Function[I, struct{}] {
	return FuncIO[I, struct{}](name, func(ctx context.Context, i I) (struct{}, error) {
		return struct{}{}, do(ctx, i)
	})
}
func FuncO[O any](name string, do func(context.Context) (O, error)) *Function[struct{}, O] {
	return FuncIO[struct{}, O](name, func(ctx context.Context, _ struct{}) (O, error) {
		return do(ctx)
	})
}
func Func(name string, do func(context.Context) error) *Function[struct{}, struct{}] {
	return FuncIO[struct{}, struct{}](name, func(ctx context.Context, _ struct{}) (struct{}, error) {
		return struct{}{}, do(ctx)
	})
}

type Function[I, O any] struct {
	Name   string
	Input  I
	Output O
	do     func(context.Context, I) (O, error)
}

func (f *Function[I, O]) String() string { return f.Name }
func (f *Function[I, O]) Do(ctx context.Context) error {
	var err error
	f.Output, err = f.do(ctx, f.Input)
	return err
}
