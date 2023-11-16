package to_test

import (
	"testing"

	"github.com/Azure/go-workflow/flcore/to"
	"github.com/stretchr/testify/assert"
)

func TestNilOrEmpty(t *testing.T) {
	t.Run("should true if nil", func(t *testing.T) {
		assert.True(t, to.IsNilOrEmpty[int](nil))
	})
	t.Run("should true if empty", func(t *testing.T) {
		some := struct{ Some string }{}
		assert.True(t, to.IsNilOrEmpty(&some))
	})
	t.Run("should false if not empty", func(t *testing.T) {
		some := struct{ Some string }{"Thing"}
		assert.False(t, to.IsNilOrEmpty(&some))
	})
}

func TestIfEmpty(t *testing.T) {
	t.Run("should set nil if the value is empty", func(t *testing.T) {
		properties := to.Ptr(struct {
			Some string
		}{})
		assert.True(t, to.IfEmpty(&properties).SetTo(nil))
		assert.Nil(t, properties)
	})
	t.Run("should not set nil if the value is not empty", func(t *testing.T) {
		properties := to.Ptr(struct {
			Some string
		}{"Values"})
		assert.False(t, to.IfEmpty(&properties).SetTo(nil))
		assert.NotNil(t, properties)
		assert.Equal(t, "Values", properties.Some)
	})
	t.Run("should return false if accepts nil", func(t *testing.T) {
		assert.False(t, to.IfEmpty[int](nil).SetTo(nil))
	})
}

func TestIfNil(t *testing.T) {
	t.Run("should new value if the value is nil", func(t *testing.T) {
		var properties *struct{ Some string }
		assert.True(t, to.IfNil(&properties).New())
		assert.NotNil(t, properties)
		assert.Empty(t, properties.Some)
	})
	t.Run("should not change anything if the value is not nil", func(t *testing.T) {
		properties := to.Ptr(struct{ Some string }{"Values"})
		assert.False(t, to.IfNil(&properties).New())
		assert.False(t, to.IfNil(&properties).SetTo(&struct{ Some string }{"OtherValues"}))
		assert.NotNil(t, properties)
		assert.Equal(t, "Values", properties.Some)
	})
	t.Run("should fill value if nil", func(t *testing.T) {
		var properties *struct{ Some string }
		assert.True(t, to.IfNil(&properties).SetTo(&struct{ Some string }{"Values"}))
		assert.NotNil(t, properties)
		assert.Equal(t, "Values", properties.Some)
	})
}

func TestIfNilOrEmpty(t *testing.T) {
	t.Run("should set if nil", func(t *testing.T) {
		var properties *struct{ Some string }
		assert.True(t, to.IfNilOrEmpty(&properties).SetTo(&struct{ Some string }{"Values"}))
		assert.NotNil(t, properties)
		assert.Equal(t, "Values", properties.Some)
	})
	t.Run("should set if empty", func(t *testing.T) {
		properties := to.Ptr(struct{ Some string }{})
		assert.True(t, to.IfNilOrEmpty(&properties).SetTo(&struct{ Some string }{"Values"}))
		assert.NotNil(t, properties)
		assert.Equal(t, "Values", properties.Some)
	})
	t.Run("should not set if not empty", func(t *testing.T) {
		properties := to.Ptr(struct{ Some string }{"Values"})
		assert.False(t, to.IfNilOrEmpty(&properties).SetTo(&struct{ Some string }{"OtherValues"}))
		assert.NotNil(t, properties)
		assert.Equal(t, "Values", properties.Some)
	})
}
