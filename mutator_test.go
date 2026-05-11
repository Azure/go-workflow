package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mutFoo struct{ Field string }

func (*mutFoo) Do(context.Context) error { return nil }

type mutBar struct{}

func (*mutBar) Do(context.Context) error { return nil }

func TestMutate_matchesExactType(t *testing.T) {
	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		return nil
	})
	matched, target, b := m.applyTo(context.Background(), &mutFoo{})
	assert.True(t, matched)
	assert.NotNil(t, target)
	assert.Nil(t, b)
	assert.Equal(t, 1, called)
}

func TestMutate_skipsNonMatchingType(t *testing.T) {
	called := 0
	m := Mutate[*mutFoo](func(ctx context.Context, f *mutFoo) Builder {
		called++
		return nil
	})
	matched, target, b := m.applyTo(context.Background(), &mutBar{})
	assert.False(t, matched)
	assert.Nil(t, target)
	assert.Nil(t, b)
	assert.Equal(t, 0, called)
}
