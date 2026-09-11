package commands

import (
	"context"
	"sync"

	"github.com/Data-Corruption/Servo/internal/app"
)

func runWorker(ctx context.Context, a *app.App, ready func()) error {
	if a.Ops == nil {
		return nil
	}
	stop := context.AfterFunc(ctx, func() { _ = a.Ops.Close() })
	defer stop()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.Sched.Run(ctx) }()
	go func() { defer wg.Done(); a.Poller.Run(ctx) }()
	ready()
	<-ctx.Done()
	_ = a.Ops.Close()
	wg.Wait()
	return nil
}
