package flow

import (
	"time"

	"github.com/benbjohnson/clock"
)

type Span struct {
	Start, End time.Time
}

func (s *Span) StartSpan(clock clock.Clock) {
	s.Start = clock.Now()
}
func (s *Span) EndSpan(clock clock.Clock) {
	s.End = clock.Now()
}
