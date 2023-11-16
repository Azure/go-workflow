package workflow

import (
	"fmt"
	"sync"
	"time"
)

// stepBase is the base interface for a Step.
// Embed `StepBase` to inherit the implementation for `stepBase`.
type stepBase interface {
	fmt.Stringer
	GetStatus() StepStatus
	GetCondition() Condition
	GetRetry() *RetryOption
	GetWhen() When
	GetTimeout() time.Duration
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
//	func(*SomeTask) Do(context.Context) error	// the main logic
//	func(*SomeTask) String() string				// [optional] give this step a name
type Base struct {
	StepName string
	mutex    sync.RWMutex
	status   StepStatus
	cond     Condition
	retry    *RetryOption
	when     When
	timeout  time.Duration
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

func (b *Base) String() string                   { return b.StepName }
func (b *Base) GetCondition() Condition          { return b.cond }
func (b *Base) GetRetry() *RetryOption           { return b.retry }
func (b *Base) GetWhen() When                    { return b.when }
func (b *Base) GetTimeout() time.Duration        { return b.timeout }
func (b *Base) setCondition(cond Condition)      { b.cond = cond }
func (b *Base) setRetry(opt *RetryOption)        { b.retry = opt }
func (b *Base) setWhen(when When)                { b.when = when }
func (b *Base) setTimeout(timeout time.Duration) { b.timeout = timeout }
