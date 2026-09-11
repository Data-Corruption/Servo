package app

import (
	"context"

	"github.com/Data-Corruption/Servo/internal/ops"
)

// InitGame is called only after acquiring the service singleton lock.
func (a *App) InitGame(ctx context.Context) error {
	runner, err := ops.New(ctx, a.DB, a.Log, ops.Paths{DriversDir: a.Layout.Drivers, DataDir: a.Layout.DriverData, BackupsDir: a.Layout.Backups, AppVersion: a.BuildInfo().Version})
	if err != nil {
		return err
	}
	a.Ops = runner
	a.Poller = ops.NewPoller(runner)
	a.Sched = ops.NewScheduler(runner)
	a.AddCleanup(runner.Close)
	return nil
}
