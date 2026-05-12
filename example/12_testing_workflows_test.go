package flow_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	flow "github.com/Azure/go-workflow"
)

// # Testing workflows
//
// **What you'll learn**
//   - Use `flow.Mock(step, fn)` to swap a Step's `Do` for a test double.
//     The original Step's identity, name, and config are preserved — only
//     `Do` is replaced. Pointer-based lookups (`As[T]` / `HasStep`) still
//     find it.
//   - You can mock Steps in a Workflow that was built by production code:
//     just hand the Workflow to your test, then call `Add(flow.Mock(...))`
//     to swap behaviours before running.
//
// **When to reach for what**
//
//	You wrote the Workflow in your test           ─► substitute Steps directly,
//	                                                 no need for Mock.
//	Production code built the Workflow            ─► flow.Mock to swap one Step.
//	You want to assert on per-Step error/status   ─► see 11_debugging.
//	You want to assert on Begin/End ordering      ─► add a StepInterceptor in
//	                                                 the test (10_observability).

// ExampleMock shows the typical use: a production workflow assembled
// elsewhere, with one Step substituted in the test.
func ExampleMock() {
	// Pretend this Workflow comes from production code we don't control.
	w := buildPipeline()

	// In our test we don't actually want to call the real `publish`. Swap it.
	w.Add(
		flow.Mock(publishStep, func(ctx context.Context) error {
			fmt.Println("(mocked publish)")
			return nil
		}),
	)

	_ = w.Do(context.Background())
	// Output:
	// build
	// (mocked publish)
}

// publishStep is the production Step we'll mock in the example.
var publishStep = flow.Func("publish", func(ctx context.Context) error {
	// Pretend this hits a real registry — we don't want it to run in tests.
	return errors.New("real publish hit; should have been mocked")
})

func buildPipeline() *flow.Workflow {
	build := flow.Func("build", func(ctx context.Context) error {
		fmt.Println("build")
		return nil
	})
	w := new(flow.Workflow)
	w.Add(flow.Pipe(build, publishStep))
	return w
}

// TestMyPipeline_unitTest demonstrates the same idea inside a real `go test`
// function (rather than as a godoc Example). This is what your CI test
// would look like; the godoc Example above just exists to show the pattern.
func TestMyPipeline_unitTest(t *testing.T) {
	called := false
	w := buildPipeline()
	w.Add(
		flow.Mock(publishStep, func(ctx context.Context) error {
			called = true
			return nil
		}),
	)
	if err := w.Do(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("publish mock was not invoked")
	}
}
