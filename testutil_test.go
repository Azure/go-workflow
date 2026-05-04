package flow

import "time"

// durationPtr returns a pointer to the given Duration value.
// Used in tests where StepOption fields require *time.Duration.
func durationPtr(d time.Duration) *time.Duration { return &d }
