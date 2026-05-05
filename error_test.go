package flow_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	flow "github.com/Azure/go-workflow"
	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrCycleDependency(t *testing.T) {
	w := new(flow.Workflow).Add(
		flow.Step(succeededStep).DependsOn(succeededStep),
	)
	var errCycle flow.ErrCycleDependency
	if assert.ErrorAs(t, w.Do(context.Background()), &errCycle) {
		assert.ErrorContains(t, errCycle, "Succeeded depends on [\n\t\tSucceeded\n\t]")
	}
}

// serialStep is a controllable step: signals when started, waits to be released.
type serialStep struct {
	name    string
	started chan struct{}
	release chan struct{}
	err     error
}

func newSerialStep(name string, err error) *serialStep {
	return &serialStep{
		name:    name,
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		err:     err,
	}
}
func (s *serialStep) Do(_ context.Context) error {
	s.started <- struct{}{}
	<-s.release
	return s.err
}
func (s *serialStep) String() string { return s.name }

func TestErrWorkflowOutputOrdering(t *testing.T) {
	// Serial chain: stepC -> stepA -> stepB
	// Names chosen so alphabetical order != execution order.
	mockClock := clock.NewMock()
	stepC := newSerialStep("C-first", fmt.Errorf("C failed"))
	stepA := newSerialStep("A-second", fmt.Errorf("A failed"))
	stepB := newSerialStep("B-third", fmt.Errorf("B failed"))

	w := &flow.Workflow{Clock: mockClock}
	w.Add(
		flow.Step(stepC),
		flow.Step(stepA).DependsOn(stepC).When(flow.Always),
		flow.Step(stepB).DependsOn(stepA).When(flow.Always),
	)

	done := make(chan error, 1)
	go func() { done <- w.Do(context.Background()) }()

	<-stepC.started
	mockClock.Add(time.Second)
	close(stepC.release)

	<-stepA.started
	mockClock.Add(time.Second)
	close(stepA.release)

	<-stepB.started
	mockClock.Add(time.Second)
	close(stepB.release)

	err := <-done
	require.Error(t, err)

	var errW flow.ErrWorkflow
	require.ErrorAs(t, err, &errW)

	output := errW.Error()
	posC := strings.Index(output, "C-first")
	posA := strings.Index(output, "A-second")
	posB := strings.Index(output, "B-third")

	assert.Greater(t, posA, posC, "A-second should appear after C-first")
	assert.Greater(t, posB, posA, "B-third should appear after A-second")
}

func TestErrWorkflowTieBreakByName(t *testing.T) {
	// Two parallel steps fail at the same clock tick → alphabetical order.
	mockClock := clock.NewMock()
	stepZ := newSerialStep("Z-step", fmt.Errorf("Z failed"))
	stepA := newSerialStep("A-step", fmt.Errorf("A failed"))

	w := &flow.Workflow{Clock: mockClock}
	w.Add(flow.Step(stepZ), flow.Step(stepA))

	done := make(chan error, 1)
	go func() { done <- w.Do(context.Background()) }()

	<-stepZ.started
	<-stepA.started
	mockClock.Add(time.Second) // both get same FinishedAt
	close(stepZ.release)
	close(stepA.release)

	err := <-done
	require.Error(t, err)

	var errW flow.ErrWorkflow
	require.ErrorAs(t, err, &errW)

	output := errW.Error()
	posA := strings.Index(output, "A-step")
	posZ := strings.Index(output, "Z-step")
	assert.Less(t, posA, posZ, "A-step should appear before Z-step (tie-break by name)")
}
