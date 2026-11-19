// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"time"
)

// Timer is a running stopwatch of a person: at most one, kept on the server so it runs on when the phone sleeps.
// Stopping it books an entry; the timer itself only knows that a clock runs.
type Timer struct {
	Owner      string    `json:"owner"`
	EmployerID string    `json:"employer_id"`
	ProjectID  string    `json:"project_id,omitempty"`
	Note       string    `json:"note,omitempty"`
	Started    time.Time `json:"started"`
}

// ErrTimerRunning is returned when a stopwatch is started while one runs.
var ErrTimerRunning = errors.New("a stopwatch is running")

// Stopwatches keep the running stopwatch of each person. Both stores of the library have them (separate from Store,
// so a store of one's own keeps working); the standalone program keeps its stopwatches next to its accounts, an
// embedding application in the store, inside its tenant.
type Stopwatches interface {
	// RunningTimer returns the stopwatch of the person, or nil.
	RunningTimer(ctx context.Context, person string) (*Timer, error)
	// StartTimer starts the stopwatch of the person; while one runs it fails with ErrTimerRunning.
	StartTimer(ctx context.Context, person string, t Timer) error
	// StopTimer removes the stopwatch of the person and returns it, or nil when none ran.
	StopTimer(ctx context.Context, person string) (*Timer, error)
}

// Validate checks a stopwatch before it is started.
func (t Timer) Validate() error {
	if err := checkRef("owner", t.Owner, true); err != nil {
		return err
	}
	if err := checkRef("employer", t.EmployerID, true); err != nil {
		return err
	}
	if err := checkRef("project", t.ProjectID, false); err != nil {
		return err
	}
	if len(t.Note) > 500 {
		return invalid("note longer than 500 characters")
	}
	if t.Started.IsZero() {
		return invalid("a stopwatch needs its start")
	}
	return nil
}
