package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMerge(t *testing.T) {
	a := newTestStep("a", StepStatusPending)
	b := newTestStep("b", StepStatusPending)
	c := newTestStep("c", StepStatusPending)

	dep1 := make(Dependency)
	dep1[a] = []Link{
		{Upstream: b},
	}
	dep2 := Dependency{
		b: []Link{
			{Upstream: c},
		},
	}
	dep1.Merge(dep2)
	assert.Len(t, dep1, 3)
	assert.ElementsMatch(t, dep1[a], []Link{{Upstream: b}})
	assert.ElementsMatch(t, dep1[b], []Link{{Upstream: c}})
	assert.Nil(t, dep1[c])
}

func TestListUpDownStream(t *testing.T) {
	a := newTestStep("a", StepStatusPending)
	b := newTestStep("b", StepStatusPending)
	c := newTestStep("c", StepStatusPending)
	d := newTestStep("d", StepStatusPending)

	dep := Dependency{
		a: []Link{
			{Upstream: b},
			{Upstream: c},
		},
		b: []Link{
			{Upstream: d},
		},
		c: []Link{
			{Upstream: d},
		},
	}

	assert.ElementsMatch(t, dep.UpstreamOf(a), []Steper{b, c})
	assert.ElementsMatch(t, dep.UpstreamOf(b), []Steper{d})
	assert.ElementsMatch(t, dep.UpstreamOf(c), []Steper{d})
	assert.ElementsMatch(t, dep.UpstreamOf(d), []Steper{})

	assert.ElementsMatch(t, dep.DownstreamOf(a), []Steper{})
	assert.ElementsMatch(t, dep.DownstreamOf(b), []Steper{a})
	assert.ElementsMatch(t, dep.DownstreamOf(c), []Steper{a})
	assert.ElementsMatch(t, dep.DownstreamOf(d), []Steper{b, c})
}
