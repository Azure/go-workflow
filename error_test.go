package flow

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type errWithMarshalJSON struct{ Err error }

func (e errWithMarshalJSON) Error() string { return e.Err.Error() }
func (e errWithMarshalJSON) Unwrap() error { return e.Err }
func (e errWithMarshalJSON) MarshalJSON() ([]byte, error) {
	return []byte(`{"msg":"err already has MarshalJSON msg"}`), nil
}

func TestStatusError(t *testing.T) {
	statusErrRandom := StatusError{
		Status: Failed,
		Err:    errors.New("mock err random msg"),
	}
	j, err := statusErrRandom.MarshalJSON()
	assert.Nil(t, err)
	assert.Equal(t, `{"status":"Failed","error":"mock err random msg"}`, string(j))

	statusErrSkip := StatusError{
		Status: Skipped,
		Err:    ErrSkip{errors.New("mock err skip msg")},
	}
	j, err = statusErrSkip.MarshalJSON()
	assert.Nil(t, err)
	assert.Equal(t, `{"status":"Skipped","error":"mock err skip msg"}`, string(j))

	statusErrWithMarshalJSON := StatusError{
		Status: Failed,
		Err:    errWithMarshalJSON{errors.New("")},
	}
	j, err = statusErrWithMarshalJSON.MarshalJSON()
	assert.Nil(t, err)
	assert.Equal(t, `{"status":"Failed","error":{"msg":"err already has MarshalJSON msg"}}`, string(j))
}

func TestErrWorkflow(t *testing.T) {
	errWorkflow := ErrWorkflow{
		&fakeStep{}: StatusError{
			Status: Failed,
			Err:    errors.New("mock err random msg"),
		},
	}
	assert.Equal(t, "*flow.fakeStep(&{}): [Failed]\n\tmock err random msg\n", errWorkflow.Error())
	assert.Equal(t, errors.New("mock err random msg"), errWorkflow.Unwrap()[0])
	j, err := errWorkflow.MarshalJSON()
	assert.Nil(t, err)
	assert.Equal(t, `{"*flow.fakeStep(\u0026{})":{"status":"Failed","error":"mock err random msg"}}`, string(j))
}

func TestErrUnexpectStepInitStatus(t *testing.T) {
	errUnexpectStepInitStatus := ErrUnexpectStepInitStatus{
		&fakeStep{}: Failed,
	}
	assert.Equal(t, "Unexpect Step initial status:\n*flow.fakeStep(&{}) [Failed]", errUnexpectStepInitStatus.Error())
}

func TestErrCycleDependency(t *testing.T) {
	errCycleDependency := ErrCycleDependency{
		&fakeStep{Name: "step2"}: {
			&fakeStep{Name: "step1"},
		},
	}
	assert.Equal(t, "Cycle Dependency Error:\n*flow.fakeStep(&{step2}): [*flow.fakeStep(&{step1})]", errCycleDependency.Error())
}
