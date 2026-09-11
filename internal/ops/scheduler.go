package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/Servo/internal/driver"
	"github.com/Data-Corruption/Servo/internal/platform/database/config"
	"github.com/Data-Corruption/Servo/internal/types"
)

// Scheduler runs the daily restart window: at the configured HH:MM it
// restarts the game server, taking a backup first when backups are enabled
// (see docs/ARCHITECTURE.md). No cron syntax — it computes
// the next occurrence and sleeps, recomputing when poked (settings change)
// and after each run. It shares the Runner's single-flight mutex, so a
// scheduled window can never race a dashboard button press.
type Scheduler struct {
	runner *Runner
	wake   chan struct{}
	// pokedFlag records that the last sleep ended via Poke; only touched
	// from the Run goroutine.
	pokedFlag bool
}

func NewScheduler(r *Runner) *Scheduler {
	return &Scheduler{runner: r, wake: make(chan struct{}, 1)}
}

// Poke tells the scheduler to recompute its next run (call after settings
// changes). Never blocks.
func (s *Scheduler) Poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run blocks until ctx is cancelled. Start it in a goroutine from the
// service command only — CLI invocations must not run scheduled windows.
func (s *Scheduler) Run(ctx context.Context) {
	log := s.runner.log
	for {
		cfg, err := config.View(s.runner.db)
		if err != nil {
			log.Errorf("scheduler: failed to read config: %v", err)
			if !s.sleep(ctx, time.Minute) {
				return
			}
			continue
		}

		if !cfg.RestartEnabled && !cfg.BackupsEnabled {
			// nothing to do; wait for a poke (or recheck hourly as a backstop)
			if !s.sleep(ctx, time.Hour) {
				return
			}
			continue
		}

		window, err := nextOccurrence(cfg.RestartTime, time.Now())
		if err != nil {
			log.Errorf("scheduler: bad restart time %q: %v", cfg.RestartTime, err)
			if !s.sleep(ctx, time.Hour) {
				return
			}
			continue
		}
		log.Infof("scheduler: next window at %s", window.Format(time.RFC1123))

		// warn players ahead of the window when configured and applicable
		lead := time.Duration(cfg.NotifyLeadMinutes) * time.Minute
		if lead > 0 && time.Until(window) > lead {
			if !s.sleep(ctx, time.Until(window.Add(-lead))) {
				return
			}
			if s.poked() {
				continue // settings changed, recompute
			}
			s.notify(ctx, cfg.NotifyLeadMinutes)
		}

		if !s.sleep(ctx, time.Until(window)) {
			return
		}
		if s.poked() {
			continue
		}
		if time.Since(window) > 5*time.Second {
			log.Warnf("scheduler: missed window skipped")
			continue
		}
		s.fire(cfg)
	}
}

// sleep waits for d (>=0). Returns false when ctx is done. A poke ends the
// sleep early; poked() reports it.
func (s *Scheduler) sleep(ctx context.Context, d time.Duration) bool {
	if d < 0 {
		d = 0
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-s.wake:
		s.pokedFlag = true
		return true
	case <-t.C:
		return true
	}
}

func (s *Scheduler) poked() bool {
	p := s.pokedFlag
	s.pokedFlag = false
	return p
}

// notify best-effort warns in-game players. Skipped when the runner is busy
// or the server is offline; unsupported verbs are silently fine.
func (s *Scheduler) notify(ctx context.Context, minutes int) {
	s.runner.Probe(func(probeCtx context.Context) {
		env, err := s.runner.Env()
		if err != nil {
			return
		}
		info, err := driver.Describe(probeCtx, env)
		if err != nil || !info.Supports(driver.VerbNotify) {
			return
		}
		status, err := driver.GetStatus(probeCtx, env)
		if err != nil || status != driver.StatusOnline {
			return
		}
		_, err = driver.RunOptional(probeCtx, env, driver.VerbNotify, fmt.Sprintf("Server maintenance in %d minutes", minutes))
		if err != nil && !errors.Is(err, driver.ErrUnsupported) && probeCtx.Err() == nil {
			s.runner.log.Warnf("player notification: %v", err)
		}
	})
}
func (s *Scheduler) fire(_ *types.Configuration) { s.startAndWait(OpWindow) }
func (s *Scheduler) startAndWait(op Op) {
	done, err := s.runner.Start(op)
	if err != nil {
		s.runner.log.Warnf("scheduled window skipped: %v", err)
		return
	}
	select {
	case <-done:
	case <-s.runner.ctx.Done():
	}
}

// Search civil dates, skipping missing local times and choosing the first
// instant of a repeated wall-clock minute. Never catch up an elapsed window.
func nextOccurrence(hhmm string, now time.Time) (time.Time, error) {
	wall, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}, err
	}
	for day := 0; day < 370; day++ {
		date := time.Date(now.Year(), now.Month(), now.Day()+day, 12, 0, 0, 0, now.Location())
		candidate := time.Date(date.Year(), date.Month(), date.Day(), wall.Hour(), wall.Minute(), 0, 0, now.Location())
		if candidate.Hour() != wall.Hour() || candidate.Minute() != wall.Minute() || candidate.Day() != date.Day() {
			continue
		}
		// Offsets around a transition identify both possible instants, including
		// non-hour DST changes, without assuming a particular hemisphere.
		_, offset := candidate.Zone()
		for _, delta := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
			_, other := (candidate.Add(delta)).Zone()
			alternate := candidate.Add(time.Duration(offset-other) * time.Second)
			if alternate.Before(candidate) && alternate.Year() == date.Year() && alternate.YearDay() == date.YearDay() && alternate.Hour() == wall.Hour() && alternate.Minute() == wall.Minute() {
				candidate = alternate
			}
		}
		if candidate.After(now) {
			return candidate, nil
		}
	}
	return time.Time{}, fmt.Errorf("no next maintenance window")
}
func NextWindow(cfg *types.Configuration, now time.Time) time.Time {
	if !cfg.RestartEnabled && !cfg.BackupsEnabled {
		return time.Time{}
	}
	next, _ := nextOccurrence(cfg.RestartTime, now)
	return next
}
