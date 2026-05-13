package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrependSlice(t *testing.T) {
	t.Run("parent nil returns child slice unchanged-shape", func(t *testing.T) {
		child := []int{1, 2}
		got := prependSlice[int](nil, child)
		assert.Equal(t, []int{1, 2}, got)
	})

	t.Run("child nil returns parent-prepended fresh slice", func(t *testing.T) {
		parent := []int{1, 2}
		got := prependSlice[int](parent, nil)
		assert.Equal(t, []int{1, 2}, got)
	})

	t.Run("both populated: parent prepended to child", func(t *testing.T) {
		parent := []int{1, 2}
		child := []int{3, 4}
		got := prependSlice[int](parent, child)
		assert.Equal(t, []int{1, 2, 3, 4}, got)
	})

	t.Run("does not mutate parent or child", func(t *testing.T) {
		parent := []int{1, 2}
		child := []int{3, 4}
		_ = prependSlice[int](parent, child)
		assert.Equal(t, []int{1, 2}, parent, "parent must not be mutated")
		assert.Equal(t, []int{3, 4}, child, "child must not be mutated")
	})

	t.Run("returned slice has a fresh backing array when both non-empty", func(t *testing.T) {
		parent := []int{1, 2}
		child := []int{3, 4}
		got := prependSlice[int](parent, child)
		got[0] = 99
		assert.Equal(t, []int{1, 2}, parent)
		assert.Equal(t, []int{3, 4}, child)
	})
}
