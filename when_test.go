package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeStep struct{}

func (fakeStep) Do(context.Context) error {
	return nil
}

func TestAlways(t *testing.T) {
	stepStatus := Always(context.Background(), map[Steper]StatusError{
		fakeStep{}: {},
	})
	assert.Equal(t, Running, stepStatus)
}

func TestAllSucceeded(t *testing.T) {
	stepStatus := AllSucceeded(context.Background(), map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Succeeded},
	})
	assert.Equal(t, Running, stepStatus)

	stepStatus = AllSucceeded(context.Background(), map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Failed},
	})
	assert.Equal(t, Skipped, stepStatus)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stepStatus = AllSucceeded(ctx, map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Succeeded},
	})
	assert.Equal(t, Canceled, stepStatus)
}

func TestBeCanceled(t *testing.T) {
	stepStatus := BeCanceled(context.Background(), map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Succeeded},
	})
	assert.Equal(t, Skipped, stepStatus)

	stepStatus = BeCanceled(context.Background(), map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Canceled},
		fakeStep{}: {Status: Failed},
	})
	assert.Equal(t, Skipped, stepStatus)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stepStatus = BeCanceled(ctx, map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Succeeded},
	})
	assert.Equal(t, Running, stepStatus)
}

func TestAnyFailed(t *testing.T) {
	stepStatus := AnyFailed(context.Background(), map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Succeeded},
	})
	assert.Equal(t, Skipped, stepStatus)

	stepStatus = AnyFailed(context.Background(), map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Skipped},
		fakeStep{}: {Status: Pending},
		fakeStep{}: {Status: StepStatus("unknown")},
		fakeStep{}: {Status: Failed},
	})
	assert.Equal(t, Running, stepStatus)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stepStatus = AnyFailed(ctx, map[Steper]StatusError{
		fakeStep{}: {Status: Succeeded},
		fakeStep{}: {Status: Failed},
	})
	assert.Equal(t, Canceled, stepStatus)
}
