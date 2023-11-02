package workflow

import (
	"context"
	"fmt"
	"reflect"
)

// FuncIO constructs a Step from an arbitrary function
func FuncIO[I, O any](name string, do func(context.Context, I) (func(*O), error)) *func_[I, O] {
	f := &func_[I, O]{do: do}
	f.Name = name
	return f
}

func FuncI[I any](name string, do func(context.Context, I) error) *func_[I, struct{}] {
	return FuncIO[I, struct{}](name, func(ctx context.Context, i I) (func(*struct{}), error) {
		return nil, do(ctx, i)
	})
}

func FuncO[O any](name string, do func(context.Context) (func(*O), error)) *func_[struct{}, O] {
	return FuncIO[struct{}, O](name, func(ctx context.Context, _ struct{}) (func(*O), error) {
		return do(ctx)
	})
}

func Func(name string, do func(context.Context) error) *func_[struct{}, struct{}] {
	return FuncIO[struct{}, struct{}](name, func(ctx context.Context, s struct{}) (func(*struct{}), error) {
		return nil, do(ctx)
	})
}

type func_[I, O any] struct {
	Base
	input  I
	do     func(context.Context, I) (func(*O), error)
	output func(*O)
}

func (f *func_[I, O]) String() string {
	if f.Name != "" {
		return f.Name
	}
	return fmt.Sprintf("Func(%s->%s)", typeOf[I](), typeOf[O]())
}

func (f *func_[I, O]) Do(ctx context.Context) error {
	var err error
	f.output, err = f.do(ctx, f.input)
	return err
}

func (f *func_[I, O]) Input() *I { return &f.input }
func (f *func_[I, O]) Output(o *O) {
	if f.output != nil {
		f.output(o)
	}
}

func typeOf[A any]() reflect.Type {
	return reflect.TypeOf(*new(A))
}
