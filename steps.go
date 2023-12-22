package flow

import (
	"context"
	"fmt"
)

// built in steps

// this will be used as a virtual node to handle the dependency of steps
type NonOpStep struct {
	Name string
}

func (n *NonOpStep) String() string {
	return fmt.Sprintf("NonOp(%s)", n.Name)
}

// Do implements Steper.
func (*NonOpStep) Do(context.Context) error {
	return nil
}

var _ Steper = (*NonOpStep)(nil)
