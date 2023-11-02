package workflow

import (
	"fmt"
	"sync"
	"time"
)

// StepReader allows you to read the status of a Step.
type StepReader interface {
	fmt.Stringer
	GetStatus() StepStatus
	GetCondition() Condition
	GetRetry() *RetryOption
	GetWhen() When
	GetTimeout() time.Duration
}

// stepBase is the base interface for a Step.
// Embed `StepBase` to inherit the implementation for `stepBase`.
type stepBase interface {
	StepReader
	setStatus(StepStatus)
	setCondition(Condition)
	setRetry(*RetryOption)
	setWhen(When)
	setTimeout(time.Duration)
}

var _ stepBase = &Base{}

// Base is to be embedded into your Step implement struct.
//
//	type SomeTask struct {
//		Base
//		... // other fields
//	}
//
// Please implement the following methods to make your struct a valid Step:
//
//	func(*SomeTask) Input() *I					// accept input by returning a reference to it
//	func(*SomeTask) Output(*O)					// send output by filling the result to the reference
//	func(*SomeTask) Do(context.Context) error	// the main logic
//	func(*SomeTask) String() string				// [optional] give this step a name
//
// Also check the whole family of Base types.
//
//   - BaseIO[I, O]: with default Input() *I and Output(*O) implement
//   - BaseEmptyIO : BaseIO[strcut{}, struct{}], means empty Input or Output
type Base struct {
	Name    string
	mutex   sync.RWMutex
	status  StepStatus
	cond    Condition
	retry   *RetryOption
	when    When
	timeout time.Duration
}

func (b *Base) String() string {
	return b.Name
}

func (b *Base) GetStatus() StepStatus {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.status
}

func (b *Base) setStatus(status StepStatus) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.status = status
}

func (b *Base) GetCondition() Condition {
	return b.cond
}

func (b *Base) setCondition(cond Condition) {
	b.cond = cond
}

func (b *Base) GetRetry() *RetryOption {
	return b.retry
}

func (b *Base) setRetry(opt *RetryOption) {
	b.retry = opt
}

func (b *Base) GetWhen() When {
	return b.when
}

func (b *Base) setWhen(when When) {
	b.when = when
}

func (b *Base) GetTimeout() time.Duration {
	return b.timeout
}

func (b *Base) setTimeout(timeout time.Duration) {
	b.timeout = timeout
}

// BaseIO[I, O] is to be embedded into your Step implement struct,
// with default Input() *I and Output(*O) implement.
type BaseIO[I, O any] struct {
	Base
	In  I
	Out O
}

func (i *BaseIO[I, O]) Input() *I     { return &i.In }
func (i *BaseIO[I, O]) Output(out *O) { *out = i.Out }

// BaseEmptyIO is to be embedded into your Step implement struct,
// indicates this Step has empty Input and Output.
type BaseEmptyIO = BaseIO[struct{}, struct{}]

// GetOutput gets the output from a Step.
func GetOutput[A any](out outputer[A]) A {
	var v A
	out.Output(&v)
	return v
}
