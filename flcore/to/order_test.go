package to_test

import (
	"testing"

	"github.com/Azure/go-workflow/flcore/to"
	"github.com/stretchr/testify/assert"
)

func TestCoalesce(t *testing.T) {
	assert.Equal(t, "hello", to.Coalesce("", "", "hello", "", "world"))

	empty := struct{ Some string }{""}
	v := struct{ Some string }{"Values"}
	assert.Equal(t, v, to.Coalesce(empty, v))

	assert.Equal(t, 123, *to.Coalesce(nil, to.Ptr(123), nil, to.Ptr(456)))

	assert.Empty(t, to.Coalesce[string]())
	assert.Empty(t, to.Coalesce[string]("", ""))
	assert.Nil(t, to.Coalesce[*int]())
	assert.Nil(t, to.Coalesce[*int](nil, nil))
}

func TestOverride(t *testing.T) {
	hello := func(s string) string {
		return "hello " + s
	}
	hey := func(s string) string {
		return "hey! " + s
	}
	assert.Equal(t, "hey! hello world", to.Override(hello, hey)("world"))
}
