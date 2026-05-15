package flow_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	flow "github.com/Azure/go-workflow"
)

// # Passing values through ctx
//
// **What you'll learn**
//   - Use `flow.ContextKey[T]` to put a typed value in `ctx` once and read
//     it in any Steper — the everyday pattern for sharing things like a
//     request ID, tenant, logger or client across a Workflow.
//   - Reach for the prebuilt `flow.Logger` key (`ContextKey[*slog.Logger]`)
//     when the value is a structured logger; combine with
//     `flow.LogStepFields` / `flow.LogAttemptField` to tag every log line
//     with the current step name and attempt number for free.

// UserKey is the canonical key for a request-scoped user. Declare one
// `ContextKey[T]` per value type — uniqueness is by T, so two such vars
// of the same T deliberately share the same key.
var UserKey = flow.ContextKey[string]{}

// ExampleContextKey shows the bare-bones pattern: caller injects, step reads.
func ExampleContextKey() {
	greet := flow.Func("Greet", func(ctx context.Context) error {
		fmt.Println("hello", UserKey.FromOr(ctx, "anonymous"))
		return nil
	})

	w := new(flow.Workflow)
	w.Add(flow.Step(greet))

	ctx := UserKey.With(context.Background(), "ada")
	_ = w.Do(ctx)
	// Output:
	// hello ada
}

// ExampleLogger shows the prebuilt flow.Logger key together with the two
// log interceptors. The step's Do() only logs business attributes;
// step=Greet and attempt=0 come along for free.
func ExampleLogger() {
	// Plain text logger to stdout, with time/level stripped so // Output:
	// stays deterministic. Real callers just pass slog.Default() or their
	// own *slog.Logger.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	}))

	greet := flow.Func("Greet", func(ctx context.Context) error {
		flow.Logger.FromOr(ctx, slog.Default()).Info("hi", "name", "ada")
		return nil
	})

	w := &flow.Workflow{
		Option: flow.WorkflowOption{
			StepInterceptors:    []flow.StepInterceptor{flow.LogStepFields()},
			AttemptInterceptors: []flow.AttemptInterceptor{flow.LogAttemptField()},
		},
	}
	w.Add(flow.Step(greet))

	ctx := flow.Logger.With(context.Background(), logger)
	_ = w.Do(ctx)
	// Output:
	//  msg=hi step=Greet attempt=0 name=ada
}
