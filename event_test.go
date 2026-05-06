package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventTypeConstants(t *testing.T) {
	// Verify all constants exist and are distinct
	types := []EventType{Scheduled, Started, Retrying, Succeeded, Failed, Canceled, Skipped}
	seen := map[EventType]bool{}
	for _, et := range types {
		assert.False(t, seen[et], "duplicate EventType: %q", et)
		seen[et] = true
	}
}

func TestStepInterceptorFunc(t *testing.T) {
	called := false
	var ic StepInterceptor = StepInterceptorFunc(func(ctx context.Context, info StepInfo, next func(context.Context) error) error {
		called = true
		return next(ctx)
	})
	_ = ic.InterceptStep(context.Background(), StepInfo{}, func(ctx context.Context) error { return nil })
	assert.True(t, called)
}

func TestAttemptInterceptorFunc(t *testing.T) {
	called := false
	var ic AttemptInterceptor = AttemptInterceptorFunc(func(ctx context.Context, info AttemptInfo, next func(context.Context) error) error {
		called = true
		return next(ctx)
	})
	_ = ic.InterceptAttempt(context.Background(), AttemptInfo{}, func(ctx context.Context) error { return nil })
	assert.True(t, called)
}
