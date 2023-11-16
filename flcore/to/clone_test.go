package to_test

import (
	"testing"

	"github.com/Azure/go-workflow/flcore/to"
	"github.com/stretchr/testify/assert"
)

func TestClone(t *testing.T) {
	a := &struct{ Some string }{"Value"}
	b := to.Clone(a)
	assert.False(t, a == b)
	assert.Equal(t, a.Some, b.Some)
	c := to.Clone(a, func(d *struct{ Some string }) {
		d.Some = "OtherValue"
	})
	assert.Equal(t, "Value", a.Some)
	assert.Equal(t, "OtherValue", c.Some)
}
