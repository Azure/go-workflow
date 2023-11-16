package reconcile

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/go-workflow/flcore/to"
	"github.com/Azure/go-workflow/fsm"
	"github.com/benbjohnson/clock"
	"github.com/cenkalti/backoff/v4"
)

// Reconcile is a process that put an Azure resource into desired goal state.
//
//	        START
//	┌─────┐   │
//	│     ▼   │
//	│   ┌─────▼─────┐
//	└─R─┤ Preflight ├─────────┐
//	    └─────┬──┬──┘         │
//	      ▲   │  │            │
//	┌─────┘   │  └─R─┐        │
//	│        1st     │        │
//	│         │  ┌───▼──────┐ │  ┌───────────┐
//	│         │  │UpdateGoal│ ├──► Succeeded │
//	│         │  └───┬──────┘ │  └───────────┘
//	│         │      │        │
//	│ ┌─────┐ │  ┌─R─┘        │  ┌────────┐
//	│ │     ▼ │  ▼            ├──► Failed │
//	│ │    ┌──▼──┐            │  └────────┘
//	│ └─R──┤ Put ├────────────┤
//	│      └──┬──┘            │
//	│         │               │
//	│      ┌──▼───┐           │
//	└─R────┤ Poll ├───────────┘
//	       └──────┘
//
// T: the type of the resource, i.e. armcontainerservice.ManagedCluster
// P: the type of the poller response, i.e. armcontainerservice.ManagedClustersClientCreateOrUpdateResponse
type Reconcile[T, P any] struct {
	// NoPoll indicates whether to wait for the long-running operation to finish.
	// Defaults to wait for the operation to complete.
	NoPoll bool
	// Frequency is the time to wait between polling intervals in absence of a Retry-After header. Allowed minimum is one second.
	// Pass zero to accept the default value (30s).
	Frequency time.Duration
	// RetryOptions configures how each phase retry.
	RetryOptions RetryOptions
	// Check defines how to check the result and make decision of each phase.
	Check CheckFunc[T, P]
	// Clock is for unit test
	Clock clock.Clock
}

type (
	// GetFunc checks the remote resource state to determine whether the resource is ready to be Put.
	// returns the current remote state, and error if any.
	GetFunc[T any] func(ctx context.Context) (remoteState *T, err error)
	// UpdateGoalFunc updates the local goal state based on the remote state.
	UpdateGoalFunc[T any] func(remoteState *T)
	// PutFunc put the <goal state> to remote, returns a poller to poll the long-running operation.
	PutFunc[T, P any] func(ctx context.Context, remoteState *T) (poller *runtime.Poller[P], err error)
	// PollerFunc use the poller from Put, to poll the long-running operation until a final state is reached,
	// returns the final remote state, and error if any.
	PollFunc[T, P any] func(ctx context.Context, poller *runtime.Poller[P]) (finalState *T, err error)

	// PreflightCheckFunc checks the remote state from Get, and make decision.
	PreflightCheckFunc[T any] func(ctx context.Context, attempt int, retryFromPoll bool, remoteState *T, err error) Decision
	// PutCheckFunc checks the result of Put, and make decision.
	PutCheckFunc[T, P any] func(ctx context.Context, attempt int, poller *runtime.Poller[P], err error) Decision
	// PollerCheckFunc checks the result of Poll, and make decision.
	PollCheckFunc[T, P any] func(ctx context.Context, attempt int, err error) Decision

	// OperationFunc really do the operations (Get / Put / Poll) towards Azure.
	OperationFunc[T, P any] struct {
		Get        GetFunc[T]
		Put        PutFunc[T, P]
		Poll       PollFunc[T, P]
		UpdateGoal UpdateGoalFunc[T]
	}

	// CheckFunc checks the result of the operation, and make decision.
	CheckFunc[T, P any] struct {
		Preflight PreflightCheckFunc[T]
		Put       PutCheckFunc[T, P]
		Poll      PollCheckFunc[T, P]
	}

	// When RetryOption is not specified (== nil), each state will only executed once.
	RetryOptions struct {
		Preflight *RetryOption
		Put       *RetryOption
		Poll      *RetryOption // Poll retry will start over
	}
)

type Preflight[T, P any] struct {
	fsm.State
	*RetryOption

	// Output
	RemoteState to.Result[*T]

	// when reconcile starts, the value is false, means we can just [Put] the local goal state to remote.
	// since first time [Preflight] transitions to [Put],
	// if we hit [Preflight] again, meaning [Poll] has been failed and we're retrying,
	// then we should jump to [UploadGoal] to update the local goal state with the latest remote state,
	// then using the updated local goal state to [Put] the resource.
	retryFromPoll bool

	Get   GetFunc[T]
	Check PreflightCheckFunc[T]
}

func (p *Preflight[T, P]) Do(ctx context.Context) fsm.Transition {
	// p.RemoteState.Err is the error from last time
	if rv, pass := p.RetryOption.check(p.RemoteState.Err); !pass {
		return rv
	}

	p.RemoteState = to.ResultOf(p.Get(ctx))
	dec := p.Check(ctx, p.attempt, p.retryFromPoll, p.RemoteState.Value, p.RemoteState.Err)
	switch {
	case dec.IsRetry:
		dec.Transition = fsm.TransitionTo[*Preflight[T, P]]()
		dec.retryOption = p.RetryOption
	case dec.IsContinue:
		p.RetryOption.Reset()
		if !p.retryFromPoll {
			dec.Transition = fsm.TransitionTo[*Put[T, P]](func(ctx context.Context, put *Put[T, P]) {
				put.RemoteState = p.RemoteState.Value
			})
		} else {
			dec.Transition = fsm.TransitionTo[*UpdateGoal[T, P]](func(ctx context.Context, ug *UpdateGoal[T, P]) {
				ug.NewGoal = p.RemoteState.Value
			})
		}
	}
	return dec
}

type UpdateGoal[T, P any] struct {
	fsm.State

	NewGoal *T

	UpdateGoal UpdateGoalFunc[T]
}

func (u *UpdateGoal[T, P]) Do(ctx context.Context) fsm.Transition {
	if u.UpdateGoal != nil {
		u.UpdateGoal(u.NewGoal)
	}
	return fsm.TransitionTo[*Put[T, P]](func(ctx context.Context, p *Put[T, P]) {
		p.RemoteState = u.NewGoal
	})
}

type Put[T, P any] struct {
	fsm.State
	*RetryOption

	// Input
	RemoteState *T

	// Output
	Poller to.Result[*runtime.Poller[P]]

	// NoPoll indicates whether to wait for the long-running operation to finish.
	// if NoPoll is true, [Put] will transiant to [Succeeded] or [Failed] directly based on whether having error.
	NoPoll bool

	Put   PutFunc[T, P]
	Check PutCheckFunc[T, P]
}

func (p *Put[T, P]) Do(ctx context.Context) fsm.Transition {
	if rv, pass := p.RetryOption.check(p.Poller.Err); !pass {
		return rv
	}

	p.Poller = to.ResultOf(p.Put(ctx, p.RemoteState))
	dec := p.Check(ctx, p.attempt, p.Poller.Value, p.Poller.Err)
	switch {
	case dec.IsRetry:
		dec.Transition = fsm.TransitionTo[*Put[T, P]]()
		dec.retryOption = p.RetryOption
	case dec.IsContinue:
		p.RetryOption.Reset()
		switch {
		case p.NoPoll && dec.Err == nil:
			dec.Transition = fsm.TransitionTo[*Succeeded[T]](func(ctx context.Context, s *Succeeded[T]) {
				s.FinalState = p.RemoteState
			})
		case p.NoPoll && dec.Err != nil:
			dec.Transition = fsm.TransitionTo[*Failed](func(ctx context.Context, f *Failed) {
				f.Err = dec.Err
			})
		default:
			dec.Transition = fsm.TransitionTo[*Poll[T, P]](func(ctx context.Context, poll *Poll[T, P]) {
				poll.Poller = p.Poller.Value
			})
		}
	}
	return dec
}

type Poll[T, P any] struct {
	fsm.State
	*RetryOption

	// Input
	Poller *runtime.Poller[P]

	// Output
	FinalState to.Result[*T]

	Poll  PollFunc[T, P]
	Check PollCheckFunc[T, P]
}

func (p *Poll[T, P]) Do(ctx context.Context) fsm.Transition {
	if rv, pass := p.RetryOption.check(p.FinalState.Err); !pass {
		return rv
	}

	p.FinalState = to.ResultOf(p.Poll(ctx, p.Poller))
	dec := p.Check(ctx, p.attempt, p.FinalState.Err)
	switch {
	case dec.IsRetry:
		dec.Transition = fsm.TransitionTo[*Preflight[T, P]](func(ctx context.Context, p *Preflight[T, P]) {
			p.retryFromPoll = true // retry poll will back to preflight again
		})
		dec.retryOption = p.RetryOption
	case dec.IsContinue:
		dec.Transition = fsm.TransitionTo[*Succeeded[T]](func(ctx context.Context, s *Succeeded[T]) {
			s.FinalState = p.FinalState.Value
		})
	}
	return dec
}

type Succeeded[T any] struct {
	fsm.EndState
	FinalState *T
}

type Failed struct {
	fsm.EndState
	Err error
}

func runOnceIfNil(opt *RetryOption) *RetryOption {
	if opt != nil {
		return opt
	}
	return &RetryOption{MaxAttempts: 1}
}

func (r *Reconcile[T, P]) Run(ctx context.Context, op OperationFunc[T, P]) (*T, error) {
	if r.Clock == nil {
		r.Clock = clock.New()
	}
	if op.Get == nil || op.Put == nil || (!r.NoPoll && op.Poll == nil) {
		return nil, fmt.Errorf("must provide Get, Put, and Poll (when NoPoll==false) operation functions")
	}
	if r.Check.Preflight == nil || r.Check.Put == nil || (!r.NoPoll && r.Check.Poll == nil) {
		return nil, fmt.Errorf("must provide Check.Preflight, Check.Put, and Check.Poll (when NoPoll==false) functions")
	}

	preflight := &Preflight[T, P]{
		RetryOption: runOnceIfNil(r.RetryOptions.Preflight),
		Get:         op.Get,
		Check:       r.Check.Preflight,
	}
	updateGoal := &UpdateGoal[T, P]{
		UpdateGoal: op.UpdateGoal,
	}
	put := &Put[T, P]{
		RetryOption: runOnceIfNil(r.RetryOptions.Put),
		NoPoll:      r.NoPoll,
		Put:         op.Put,
		Check:       r.Check.Put,
	}
	poll := &Poll[T, P]{
		RetryOption: runOnceIfNil(r.RetryOptions.Poll),
		Poll:        op.Poll,
		Check:       r.Check.Poll,
	}
	failed := &Failed{}
	succeeded := &Succeeded[T]{}

	stateMachine, err := fsm.NewStateMachine(preflight, updateGoal, put, poll, failed, succeeded)
	if err != nil {
		return nil, err
	}
	stateMachine.Clock = r.Clock

	end, err := stateMachine.Start(ctx, preflight)
	if err != nil {
		return nil, err
	}
	switch end {
	case succeeded:
		return succeeded.FinalState, nil
	case failed:
		return nil, failed.Err
	}
	return nil, fmt.Errorf("unexpected end state: %s", reflect.TypeOf(end))
}

func (r *Reconcile[T, P]) PollUntilDoneOptions() *runtime.PollUntilDoneOptions {
	if r.Frequency == 0 {
		return nil
	}
	return &runtime.PollUntilDoneOptions{Frequency: r.Frequency}
}

type RetryOption struct {
	MaxAttempts    int
	DefaultBackOff backoff.BackOff
	attempt        int
}

func (opt *RetryOption) NextBackOff() time.Duration {
	opt.attempt++
	if opt.DefaultBackOff != nil {
		return opt.DefaultBackOff.NextBackOff()
	}
	return 0
}

func (opt *RetryOption) check(err error) (fsm.Transition, bool) {
	if opt.MaxAttempts > 0 && opt.attempt >= opt.MaxAttempts {
		return fsm.TransitionTo[*Failed](func(ctx context.Context, f *Failed) {
			f.Err = err
		}), false
	}
	return nil, true
}

func (opt *RetryOption) Reset() {
	opt.attempt = 0
	if opt.DefaultBackOff != nil {
		opt.DefaultBackOff.Reset()
	}
}
