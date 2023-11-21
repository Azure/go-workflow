package flow

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type someStep struct {
	value string
}

func (s *someStep) Do(ctx context.Context) error {
	fmt.Println(s.value)
	return nil
}

func TestUnwrap(t *testing.T) {
	step := &NamedStep{
		Name: "I have a name",
		Steper: &someStep{
			value: "I have a value",
		},
	}

	assert.True(t, Is[*someStep](step))
	assert.True(t, Is[*NamedStep](step))

	var s *someStep
	assert.True(t, As(step, &s))
	assert.Equal(t, "I have a value", s.value)
	assert.True(t, step.Steper == s)

	var n *NamedStep
	assert.True(t, As(step, &n))
	assert.True(t, step == n)
}

func TestChainedStep(t *testing.T) {
	step := &NamedStep{
		Name: "I have a name",
		Steper: &someStep{
			value: "I have a value",
		},
	}
	root := &rootStep{step}
	t.Run("should add all steps in Unwrap() chain", func(t *testing.T) {
		workflow := new(Workflow)
		workflow.Add(Step(root))
		assert.Len(t, workflow.dep[PhaseRun], 1, "dependency should still respect declaration")
		assert.Contains(t, workflow.dep[PhaseRun], root)
		assert.Len(t, workflow.states, 1)
		assert.Contains(t, workflow.states, root)
		assert.Len(t, workflow.chain, 3)
		assert.Contains(t, workflow.chain, root)
		assert.Contains(t, workflow.chain, step)
		assert.Contains(t, workflow.chain, step.Steper)
	})
	anotherRoot := &rootStep{root}
	t.Run("should panic if add different root step", func(t *testing.T) {
		workflow := new(Workflow)
		assert.Panics(t, func() {
			workflow.Add(Step(root, anotherRoot))
		})
	})
}

type rootStep struct{ Steper }

func (r *rootStep) Unwrap() Steper { return r.Steper }
