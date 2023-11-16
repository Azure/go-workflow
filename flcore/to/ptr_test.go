package to_test

import (
	"testing"

	"github.com/Azure/go-workflow/flcore/to"
	"github.com/stretchr/testify/assert"
)

func TestPtr(t *testing.T) {
	t.Run("should return a pointer to copy", func(t *testing.T) {
		want := struct {
			Some string
		}{"Thing"}
		got := to.Ptr(want)
		if got == &want {
			assert.Fail(t, "should return a pointer to copy")
		}
		assert.Equal(t, want, *got)
	})
	t.Run("should never return a nil pointer", func(t *testing.T) {
		want := (*struct{})(nil)
		got := to.Ptr(want)
		assert.NotNil(t, got)
	})
}

func TestDeref(t *testing.T) {
	t.Run("should return value of the pointer", func(t *testing.T) {
		want := struct {
			Some string
		}{"Thing"}
		got := to.Deref(&want)
		assert.Equal(t, want, got)
	})
	t.Run("should return a zero value if the pointer is nil", func(t *testing.T) {
		got := to.Deref[struct{ Some string }](nil)
		assert.Empty(t, got.Some)
	})
}
