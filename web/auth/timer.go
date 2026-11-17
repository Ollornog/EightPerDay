// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Timer is a running stopwatch. The program keeps it next to the accounts: the core library books the entry once the
// clock stops, and knows nothing of a clock that runs.
type Timer struct {
	EmployerID string
	ProjectID  string
	Note       string
	Started    time.Time
}

// ErrTimerRunning is returned when a stopwatch is started while one runs.
var ErrTimerRunning = errors.New("a stopwatch is running")

// RunningTimer returns the stopwatch of an account, or nil.
func (s *Service) RunningTimer(ctx context.Context, accountID string) (*Timer, error) {
	var t Timer
	err := s.db.QueryRow(ctx, "SELECT employer_id, project_id, note, started FROM epd_timer WHERE account_id = $1", accountID).
		Scan(&t.EmployerID, &t.ProjectID, &t.Note, &t.Started)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// StartTimer starts the stopwatch of an account; only one runs at a time.
func (s *Service) StartTimer(ctx context.Context, accountID string, t Timer) error {
	tag, err := s.db.Exec(ctx, `INSERT INTO epd_timer (account_id, owner, employer_id, project_id, note, started)
		VALUES ($1, $1, $2, $3, $4, $5) ON CONFLICT (account_id) DO NOTHING`, accountID, t.EmployerID, t.ProjectID, t.Note, t.Started)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTimerRunning
	}
	return nil
}

// StopTimer removes the stopwatch and returns it, or nil when none ran.
func (s *Service) StopTimer(ctx context.Context, accountID string) (*Timer, error) {
	var t Timer
	err := s.db.QueryRow(ctx, "DELETE FROM epd_timer WHERE account_id = $1 RETURNING employer_id, project_id, note, started", accountID).
		Scan(&t.EmployerID, &t.ProjectID, &t.Note, &t.Started)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
