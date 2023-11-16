package workflow

import "context"

// Notify will be called before and after each step being executed.
type Notify struct {
	BeforeStep func(ctx context.Context, step Steper) context.Context
	AfterStep  func(ctx context.Context, step Steper, err error)
}
