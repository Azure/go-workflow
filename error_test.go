package workflow

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestStep(name string, status StepStatus) *testStep {
	s := new(testStep)
	s.StepName = name
	s.setStatus(status)
	return s
}

func TestErrorWorkflow(t *testing.T) {
	t.Run("IsNil", func(t *testing.T) {
		e := ErrWorkflow{}
		assert.True(t, e.IsNil())
		e[newTestStep("", StepStatusPending)] = nil
		assert.True(t, e.IsNil())
	})
	t.Run("Error", func(t *testing.T) {
		e := ErrWorkflow{
			newTestStep("1", StepStatusPending):   nil,
			newTestStep("2", StepStatusRunning):   nil,
			newTestStep("3", StepStatusSucceeded): nil,
			newTestStep("4", StepStatusFailed):    fmt.Errorf("Step 4 failed"),
		}
		assert.Equal(t, "4 [Failed]: Step 4 failed\n", e.Error())
	})
}

func TestErrUnexpectStepInitStatus(t *testing.T) {
	s1 := newTestStep("1", StepStatusSucceeded)
	s2 := newTestStep("2", StepStatusRunning)
	s3 := newTestStep("3", StepStatusFailed)
	err := ErrUnexpectStepInitStatus{s1, s2, s3}
	assert.Equal(t, `Unexpect Step initial status:
1 [Succeeded]
2 [Running]
3 [Failed]`, err.Error())
}

func TestErrCycleDependency(t *testing.T) {
	s1 := newTestStep("1", StepStatusSucceeded)
	s2 := newTestStep("2", StepStatusRunning)
	s3 := newTestStep("3", StepStatusFailed)
	err := ErrCycleDependency{
		s1: {s2, s3},
	}
	assert.Equal(t, `Cycle Dependency Error:
1: [2, 3]`, err.Error())
}
