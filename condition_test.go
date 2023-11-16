package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testStep struct{ Base }

func (t *testStep) Do(context.Context) error { return nil }

func TestCondition(t *testing.T) {
	newTestStatus := func(status StepStatus) *testStep {
		return newTestStep("", status)
	}
	var (
		pending   = newTestStatus(StepStatusPending)
		running   = newTestStatus(StepStatusRunning)
		succeeded = newTestStatus(StepStatusSucceeded)
		failed    = newTestStatus(StepStatusFailed)
		canceled  = newTestStatus(StepStatusCanceled)
	)
	t.Run("Always", func(t *testing.T) {
		assert.Equal(t, true, Always([]Steper{
			pending, running, succeeded, failed, canceled,
		}))
	})
	t.Run("Succeeded", func(t *testing.T) {
		for _, c := range []struct {
			Name      string
			Upstreams []Steper
			Expect    bool
		}{
			{
				Name:      "Empty => true",
				Upstreams: []Steper{},
				Expect:    true,
			},
			{
				Name: "Succeeded => true",
				Upstreams: []Steper{
					succeeded,
				},
				Expect: true,
			},
			{
				Name: "Any Failed => false",
				Upstreams: []Steper{
					succeeded, failed,
				},
				Expect: false,
			},
			{
				Name: "Any Canceled => false",
				Upstreams: []Steper{
					succeeded, canceled,
				},
				Expect: false,
			},
		} {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				assert.Equal(t, c.Expect, Succeeded(c.Upstreams))
			})
		}
	})
	t.Run("Failed", func(t *testing.T) {
		for _, c := range []struct {
			Name      string
			Upstreams []Steper
			Expect    bool
		}{
			{
				Name:      "Empty => false",
				Upstreams: []Steper{},
				Expect:    false,
			},
			{
				Name: "Succeeded => false",
				Upstreams: []Steper{
					succeeded,
				},
				Expect: false,
			},
			{
				Name: "Any Failed => true",
				Upstreams: []Steper{
					succeeded, failed,
				},
				Expect: true,
			},
			{
				Name: "Any Canceled => false",
				Upstreams: []Steper{
					failed, canceled,
				},
				Expect: false,
			},
		} {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				assert.Equal(t, c.Expect, Failed(c.Upstreams))
			})
		}
	})
	t.Run("SucceededOrFailed", func(t *testing.T) {
		for _, c := range []struct {
			Name      string
			Upstreams []Steper
			Expect    bool
		}{
			{
				Name:      "Empty => true",
				Upstreams: []Steper{},
				Expect:    true,
			},
			{
				Name: "Succeeded => true",
				Upstreams: []Steper{
					succeeded,
				},
				Expect: true,
			},
			{
				Name: "Any Failed => true",
				Upstreams: []Steper{
					succeeded, failed,
				},
				Expect: true,
			},
			{
				Name: "Any Canceled => false",
				Upstreams: []Steper{
					failed, canceled,
				},
				Expect: false,
			},
		} {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				assert.Equal(t, c.Expect, SucceededOrFailed(c.Upstreams))
			})
		}
	})
	t.Run("Canceled", func(t *testing.T) {
		for _, c := range []struct {
			Name      string
			Upstreams []Steper
			Expect    bool
		}{
			{
				Name:      "Empty => false",
				Upstreams: []Steper{},
				Expect:    false,
			},
			{
				Name: "Succeeded => false",
				Upstreams: []Steper{
					succeeded,
				},
				Expect: false,
			},
			{
				Name: "Any Failed => false",
				Upstreams: []Steper{
					succeeded, failed,
				},
				Expect: false,
			},
			{
				Name: "Any Canceled => true",
				Upstreams: []Steper{
					failed, canceled,
				},
				Expect: true,
			},
		} {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				assert.Equal(t, c.Expect, Canceled(c.Upstreams))
			})
		}
	})
}
