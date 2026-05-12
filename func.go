package flow

import (
	"context"
)

// Func adapts a `func(ctx) error` into a Step (no input, no output) named
// `name`. Convenient for one-off inline steps that don't deserve their own
// type.
//
//	w.Add(flow.Step(flow.Func("greet", func(ctx context.Context) error {
//	    fmt.Println("hello")
//	    return nil
//	})))
func Func(name string, do func(context.Context) error) *Function[struct{}, struct{}] {
	return FuncIO(name, func(ctx context.Context, _ struct{}) (struct{}, error) {
		return struct{}{}, do(ctx)
	})
}

// FuncIO adapts a `func(ctx, In) (Out, error)` into a Step. The Input field
// is fed in (so you can populate it via Step(...).Input(...)) and the
// returned Out is stored on the Output field (so you can read it via
// Step(...).Output(...)).
func FuncIO[I, O any](name string, do func(context.Context, I) (O, error)) *Function[I, O] {
	f := &Function[I, O]{Name: name, DoFunc: do}
	return f
}

// FuncI is FuncIO for an input-only function (no output).
func FuncI[I any](name string, do func(context.Context, I) error) *Function[I, struct{}] {
	return FuncIO(name, func(ctx context.Context, i I) (struct{}, error) {
		return struct{}{}, do(ctx, i)
	})
}

// FuncO is FuncIO for an output-only function (no input).
func FuncO[O any](name string, do func(context.Context) (O, error)) *Function[struct{}, O] {
	return FuncIO(name, func(ctx context.Context, _ struct{}) (O, error) {
		return do(ctx)
	})
}

// Function is the Step implementation produced by Func / FuncIO / FuncI /
// FuncO. Input is supplied by the caller (typically via Step(f).Input(...)),
// passed into DoFunc on each attempt, and the return value is stashed in
// Output (so Step(f).Output(...) can pick it up). String() returns Name, so
// Function shows up nicely in logs and ErrWorkflow messages.
type Function[I, O any] struct {
	Name   string
	Input  I
	Output O
	DoFunc func(context.Context, I) (O, error)
}

func (f *Function[I, O]) String() string { return f.Name }
func (f *Function[I, O]) Do(ctx context.Context) error {
	var err error
	if f.DoFunc != nil {
		f.Output, err = f.DoFunc(ctx, f.Input)
	}
	return err
}
