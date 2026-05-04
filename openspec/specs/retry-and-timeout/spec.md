## ADDED Requirements

### Requirement: Retry is opt-in and disabled by default

A Step SHALL NOT be retried unless `.Retry(...)` is explicitly declared on it.
Passing a `nil` option function to `.Retry()` enables retry with `DefaultRetryOption`.

`DefaultRetryOption` is:
- `Attempts: 3`
- `Backoff: exponential backoff` (via `github.com/cenkalti/backoff/v4`)

#### Scenario: No retry configured
- **WHEN** a Step is added without `.Retry(...)`
- **THEN** a failed `Do` is not retried; the Step immediately transitions to `Failed`

#### Scenario: Retry with nil enables DefaultRetryOption
- **WHEN** a Step is declared with `.Retry(nil)`
- **THEN** it retries up to 3 times with exponential backoff

#### Scenario: Retry with custom options
- **WHEN** a Step is declared with `.Retry(func(ro *flow.RetryOption) { ro.Attempts = 5 })`
- **THEN** `Do` is called up to 5 times before the Step is marked `Failed`

---

### Requirement: Attempts limits the total number of Do calls

`RetryOption.Attempts` is the **total** number of attempts (first try + retries).
An `Attempts` value of 0 means unlimited retries.

#### Scenario: Attempts=3 means exactly 3 calls to Do
- **WHEN** `Attempts` is 3 and `Do` fails on every call
- **THEN** `Do` is called exactly 3 times and then the Step is marked `Failed`

#### Scenario: Attempts=0 retries indefinitely
- **WHEN** `Attempts` is 0 and `Do` always fails
- **THEN** the retry loop continues until the context is canceled or `backoff.Stop`
  is returned by the backoff strategy

---

### Requirement: Backoff controls the delay between attempts

`RetryOption.Backoff` is a `backoff.BackOff` from the `cenkalti/backoff/v4` package.
The Workflow calls `NextBackOff()` after each failed attempt to determine how long to
wait before the next attempt. When `NextBackOff()` returns `backoff.Stop`, no further
attempts are made.

#### Scenario: Backoff delay is respected
- **WHEN** a custom `Backoff` is configured with a fixed 1-second interval
- **THEN** the Workflow waits approximately 1 second between each retry attempt

---

### Requirement: NextBackOff callback for retry-aware logic

`RetryOption.NextBackOff` is an optional callback
`func(ctx context.Context, re RetryEvent, nextBackOff time.Duration) time.Duration`
that intercepts the backoff duration after each retry. The callback receives the
`RetryEvent` (attempt number, elapsed duration, last error) and the duration proposed
by the inner `Backoff`, and returns the actual duration to use.

#### Scenario: NextBackOff can shorten the backoff
- **WHEN** `NextBackOff` returns a duration shorter than the inner backoff proposed
- **THEN** the Workflow waits the shorter duration

#### Scenario: NextBackOff can stop retrying
- **WHEN** `NextBackOff` returns `backoff.Stop`
- **THEN** no further attempts are made regardless of remaining `Attempts`

---

### Requirement: TimeoutPerTry limits each individual Do call

`RetryOption.TimeoutPerTry` sets a per-attempt deadline. Each call to `Do` receives a
child context that is canceled after `TimeoutPerTry`. A value of `0` means no per-try
timeout.

#### Scenario: Per-try timeout cancels a slow Do
- **WHEN** `TimeoutPerTry` is 10 seconds and `Do` runs longer than 10 seconds
- **THEN** the context passed to `Do` is canceled and `Do` receives a deadline-exceeded error

#### Scenario: Per-try timeout resets between attempts
- **WHEN** `TimeoutPerTry` is 5 seconds and the first attempt is canceled at 5 seconds
- **THEN** the second attempt starts a fresh 5-second deadline

---

### Requirement: Step-level Timeout caps the entire retry sequence

`.Timeout(d)` sets a deadline for the **entire** Step execution including all retry
attempts and backoff waits. When the Step timeout expires, the current `Do` context is
canceled and no further retries are started.

```
|<────────────── Step Timeout ──────────────────>|
| attempt 1 |backoff| attempt 2 |backoff| attempt 3 ...
              |<────>|
           Per-Try Timeout applies to each attempt
```

#### Scenario: Step timeout cancels mid-retry
- **WHEN** the Step-level timeout expires during a retry sequence
- **THEN** the current `Do` call receives a canceled context and no new attempts start;
  the Step is set to `Canceled`

#### Scenario: Step timeout shorter than retry sequence
- **WHEN** `Timeout(15m)` is set and `TimeoutPerTry` is 10 minutes with 2 attempts
- **THEN** only attempts that fit within the 15-minute window execute

---

### Requirement: Context cancellation stops retrying

If the parent context passed to `workflow.Do` is canceled or its deadline exceeded,
the retry loop SHALL stop and the Step SHALL be set to `Canceled`, regardless of
remaining attempts.

#### Scenario: Workflow context canceled during retry
- **WHEN** the context passed to `workflow.Do` is canceled while a Step is being retried
- **THEN** the retry loop stops and the Step is set to `Canceled`

---

### Requirement: Notify callback for retry events

`RetryOption.Notify` is a `backoff.Notify` callback
`func(error, time.Duration)` called after each failed attempt before the backoff sleep.
It can be used for logging or metrics.

#### Scenario: Notify called after each failure
- **WHEN** a `Notify` callback is configured and `Do` fails on the first attempt
- **THEN** `Notify` is called with the error from that attempt and the upcoming backoff duration
