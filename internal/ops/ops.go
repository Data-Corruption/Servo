// Package ops owns the service's single-flight driver runtime.
package ops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/Servo/internal/driver"
	"github.com/Data-Corruption/Servo/internal/layout"
	"github.com/Data-Corruption/Servo/internal/platform/database/config"
	"github.com/Data-Corruption/Servo/internal/types"
	"github.com/Data-Corruption/Servo/pkg/crypto"
	"github.com/Data-Corruption/Servo/pkg/xlog"
)

type Op string

const (
	OpStart     Op = "start"
	OpStop      Op = "stop"
	OpRestart   Op = "restart"
	OpInstall   Op = "install"
	OpUpdate    Op = "update"
	OpBackup    Op = "backup"
	OpRestore   Op = "restore"
	OpUninstall Op = "uninstall"
	OpWindow    Op = "window"
)

var ValidOps = map[Op]bool{OpStart: true, OpStop: true, OpRestart: true, OpInstall: true, OpUpdate: true, OpBackup: true, OpRestore: true, OpUninstall: true}
var (
	ErrBusy         = errors.New("an operation is already running")
	ErrNoDriver     = errors.New("no active driver")
	ErrServerOnline = errors.New("server is online")
)

type Result struct {
	ID        string    `json:"id"`
	Driver    string    `json:"driver"`
	Op        Op        `json:"op"`
	Step      string    `json:"step"`
	Outcome   string    `json:"outcome"`
	Success   bool      `json:"success"`
	Detail    string    `json:"detail,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Tail      string    `json:"tail,omitempty"`
}
type Paths struct{ DriversDir, DataDir, BackupsDir, AppVersion string }
type Snapshot struct {
	Running   bool    `json:"running"`
	ID        string  `json:"id"`
	Op        Op      `json:"op,omitempty"`
	Step      string  `json:"step,omitempty"`
	ElapsedMs int64   `json:"elapsedMs,omitempty"`
	Last      *Result `json:"last,omitempty"`
}
type Runner struct {
	db          *sql.DB
	log         *xlog.Logger
	paths       Paths
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	running     bool
	closing     bool
	current     Result
	last        *Result
	buf         *ring
	gate        chan struct{}
	probeCancel context.CancelFunc
	wg          sync.WaitGroup
}

func New(ctx context.Context, db *sql.DB, log *xlog.Logger, paths Paths) (*Runner, error) {
	ctx, cancel := context.WithCancel(ctx)
	r := &Runner{db: db, log: log, paths: paths, ctx: ctx, cancel: cancel, buf: newRing(), gate: make(chan struct{}, 1)}
	var data string
	err := db.QueryRow(`SELECT value FROM game_operation WHERE id=1`).Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		cancel()
		return nil, err
	}
	if err == nil {
		var last Result
		if err = json.Unmarshal([]byte(data), &last); err != nil {
			cancel()
			return nil, err
		}
		if last.Outcome == "running" {
			last.Outcome = "interrupted"
			last.Success = false
			last.EndedAt = time.Now()
			last.Detail = "Servo stopped before this operation completed; inspect the game before retrying"
			if err = r.persist(&last); err != nil {
				cancel()
				return nil, err
			}
		}
		r.last = &last
	}
	return r, nil
}
func (r *Runner) persist(result *Result) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = r.db.ExecContext(ctx, `INSERT INTO game_operation(id,value) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value`, string(data))
	return err
}
func (r *Runner) Close() error {
	r.mu.Lock()
	r.closing = true
	r.cancel()
	if r.probeCancel != nil {
		r.probeCancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
	return nil
}
func (r *Runner) Env() (driver.Env, error) {
	cfg, err := config.View(r.db)
	if err != nil {
		return driver.Env{}, err
	}
	return r.EnvFor(cfg.ActiveDriver)
}
func (r *Runner) EnvFor(name string) (driver.Env, error) {
	if name == "" {
		return driver.Env{}, ErrNoDriver
	}
	path, err := driver.Resolve(r.paths.DriversDir, name)
	if err != nil {
		return driver.Env{}, err
	}
	l := layout.Layout{DriverData: r.paths.DataDir, Backups: r.paths.BackupsDir}
	data, backups, err := l.DriverPaths(name)
	if err != nil {
		return driver.Env{}, err
	}
	return driver.Env{DriverPath: path, DataDir: data, BackupDir: backups, AppVersion: r.paths.AppVersion}, nil
}
func (r *Runner) Busy() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.running }

// reserve marks intent before cancelling a probe, so no new probe can slip in.
func (r *Runner) reserve(ctx context.Context) error {
	r.mu.Lock()
	if r.closing || r.ctx.Err() != nil {
		r.mu.Unlock()
		return context.Canceled
	}
	if r.running {
		r.mu.Unlock()
		return ErrBusy
	}
	r.running = true
	r.current = Result{}
	r.wg.Add(1)
	if r.probeCancel != nil {
		r.probeCancel()
	}
	r.mu.Unlock()
	select {
	case r.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		r.release(false)
		return ctx.Err()
	case <-r.ctx.Done():
		r.release(false)
		return r.ctx.Err()
	}
}
func (r *Runner) release(held bool) {
	if held {
		<-r.gate
	}
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()
	r.wg.Done()
}

// Probe runs only while idle, and can be preempted by an interactive action.
func (r *Runner) Probe(fn func(context.Context)) {
	r.mu.Lock()
	if r.running || r.closing || r.ctx.Err() != nil {
		r.mu.Unlock()
		return
	}
	select {
	case r.gate <- struct{}{}:
	default:
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.probeCancel = cancel
	r.wg.Add(1)
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		r.probeCancel = nil
		r.mu.Unlock()
		<-r.gate
		r.wg.Done()
	}()
	fn(ctx)
}
func (r *Runner) Activate(ctx context.Context, name string) (driver.Info, error) {
	if err := r.reserve(ctx); err != nil {
		return driver.Info{}, err
	}
	defer r.release(true)
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	cfg, err := config.View(r.db)
	if err != nil {
		return driver.Info{}, err
	}
	if cfg.ActiveDriver != "" && cfg.ActiveDriver != name {
		env, err := r.EnvFor(cfg.ActiveDriver)
		if err != nil {
			return driver.Info{}, err
		}
		status, err := driver.GetStatus(probeCtx, env)
		if err != nil {
			return driver.Info{}, err
		}
		if status != driver.StatusOffline {
			return driver.Info{}, ErrServerOnline
		}
	}
	env, err := r.EnvFor(name)
	if err != nil {
		return driver.Info{}, err
	}
	if err := r.prepareEnv(env); err != nil {
		return driver.Info{}, err
	}
	info, missing, err := driver.Validate(probeCtx, env)
	if err != nil {
		return info, err
	}
	if len(missing) > 0 {
		return info, fmt.Errorf("missing dependencies: %s", strings.Join(missing, ", "))
	}
	if err := probeCtx.Err(); err != nil {
		return info, err
	}
	_, err = config.Update(r.db, func(cfg *types.Configuration) error { cfg.ActiveDriver = name; return nil })
	return info, err
}
func (r *Runner) Status() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked()
}

// Poll binds log offsets to the same operation identity as the snapshot.
func (r *Runner) Poll(id string, offset int64) (Snapshot, []byte, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.statusLocked()
	if id != s.ID {
		offset = 0
	}
	output, next := r.buf.ReadFrom(offset)
	return s, output, next
}

func (r *Runner) statusLocked() Snapshot {
	s := Snapshot{Running: r.running, ID: r.current.ID, Op: r.current.Op, Step: r.current.Step}
	if r.last != nil {
		last := *r.last
		s.Last = &last
		if s.ID == "" {
			s.ID = last.ID
		}
	}
	if r.running && !r.current.StartedAt.IsZero() {
		s.ElapsedMs = time.Since(r.current.StartedAt).Milliseconds()
	}
	return s
}
func (r *Runner) Tail(offset int64) ([]byte, int64) { return r.buf.ReadFrom(offset) }
func (r *Runner) Start(op Op, args ...string) (<-chan struct{}, error) {
	if !ValidOps[op] && op != OpWindow {
		return nil, fmt.Errorf("unknown operation")
	}
	if err := r.reserve(r.ctx); err != nil {
		return nil, err
	}
	env, err := r.Env()
	if err != nil {
		r.release(true)
		return nil, err
	}
	if op == OpRestore {
		if len(args) != 1 {
			r.release(true)
			return nil, fmt.Errorf("restore requires archive")
		}
		if _, err = ResolveBackup(env.BackupDir, args[0]); err != nil {
			r.release(true)
			return nil, err
		}
	}
	if err := r.prepareEnv(env); err != nil {
		r.release(true)
		return nil, err
	}
	id, err := crypto.GenRandomString(16)
	if err != nil {
		r.release(true)
		return nil, err
	}
	res := Result{ID: id, Driver: filepath.Base(env.DriverPath), Op: op, Outcome: "running", StartedAt: time.Now()}
	if err = r.persist(&res); err != nil {
		r.release(true)
		return nil, err
	}
	r.mu.Lock()
	r.current = res
	r.buf.Reset()
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { defer close(done); defer r.release(true); r.run(op, args, env) }()
	return done, nil
}
func (r *Runner) say(format string, a ...any) { fmt.Fprintf(r.buf, "[servo] "+format+"\n", a...) }
func (r *Runner) run(op Op, args []string, env driver.Env) {
	err := r.sequence(op, args, env)
	r.mu.Lock()
	res := r.current
	r.mu.Unlock()
	res.EndedAt = time.Now()
	res.Success = err == nil
	res.Outcome = "succeeded"
	if err != nil {
		res.Outcome = "failed"
		res.Detail = err.Error()
		r.say("FAILED: %v", err)
		if r.ctx.Err() != nil {
			res.Outcome = "interrupted"
		}
	} else {
		r.say("done")
	}
	res.Tail = r.buf.Tail(4096)
	if err := r.persist(&res); err != nil {
		res.Success = false
		res.Outcome = "failed"
		res.Detail = "Could not persist operation outcome: " + err.Error()
		r.log.Errorf("%s", res.Detail)
		r.mu.Lock()
		r.closing = true
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.last = &res
	r.mu.Unlock()
}
func (r *Runner) setStep(verb string) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	r.current.Step = verb
	res := r.current
	r.mu.Unlock()
	return r.persist(&res)
}
func (r *Runner) status(env driver.Env) (driver.Status, error) {
	if err := r.setStep("status"); err != nil {
		return driver.StatusUnknown, err
	}
	return driver.GetStatus(r.ctx, env)
}
func (r *Runner) stepOutput(env driver.Env, verb string, stdout io.Writer, args ...string) error {
	if err := r.setStep(verb); err != nil {
		return err
	}
	r.say("%s", verb)
	var code int
	var err error
	if stdout == nil {
		code, err = driver.Run(r.ctx, env, r.buf, verb, args...)
	} else {
		code, err = driver.RunOutput(r.ctx, env, io.MultiWriter(r.buf, stdout), r.buf, verb, args...)
	}
	if err != nil {
		return err
	}
	if code != driver.ExitOK {
		return fmt.Errorf("%s exited %d", verb, code)
	}
	return nil
}
func (r *Runner) step(env driver.Env, verb string, args ...string) error {
	return r.stepOutput(env, verb, nil, args...)
}

// sequence composes driver verbs per op. update/backup/restore preserve the
// server's prior state: they only start it afterwards if it was online before
// (never start a server someone stopped on purpose).
func (r *Runner) sequence(op Op, args []string, env driver.Env) error {
	if op == OpRestore || op == OpUninstall {
		if err := r.setStep("describe"); err != nil {
			return err
		}
		info, err := driver.Describe(r.ctx, env)
		if err != nil {
			return err
		}
		if !info.Supports(string(op)) {
			return driver.ErrUnsupported
		}
	}
	switch op {
	case OpWindow:
		cfg, err := config.View(r.db)
		if err != nil {
			return err
		}
		if cfg.BackupsEnabled {
			return r.sequence(OpBackup, nil, env)
		}
		if !cfg.RestartEnabled {
			return nil
		}
		status, err := r.status(env)
		if err != nil {
			return err
		}
		if status == driver.StatusOffline {
			return nil
		}
		return r.sequence(OpRestart, nil, env)

	case OpStart:
		return r.step(env, driver.VerbStart)
	case OpStop:
		return r.step(env, driver.VerbStop)
	case OpRestart:
		if err := r.step(env, driver.VerbStop); err != nil {
			return err
		}
		return r.step(env, driver.VerbStart)
	case OpInstall:
		status, err := r.status(env)
		if err != nil {
			return err
		}
		if status != driver.StatusOffline {
			return ErrServerOnline
		}
		return r.step(env, driver.VerbInstall)
	case OpUpdate:
		return r.stopDoStart(env, func() error {
			return r.step(env, driver.VerbUpdate)
		})
	case OpBackup:
		return r.stopDoStart(env, func() error {
			var output lastLine
			if err := r.stepOutput(env, driver.VerbBackup, &output); err != nil {
				return err
			}
			name := strings.TrimSpace(output.String())
			if !filepath.IsAbs(name) || filepath.Dir(name) != env.BackupDir {
				return fmt.Errorf("backup did not report an archive in its backup directory")
			}
			if _, err := ResolveBackup(env.BackupDir, filepath.Base(name)); err != nil {
				return err
			}
			if err := r.setStep("retention"); err != nil {
				return err
			}
			return r.pruneBackups(env.BackupDir)
		})
	case OpRestore:
		if len(args) != 1 {
			return fmt.Errorf("restore requires an archive name")
		}
		archive, err := ResolveBackup(env.BackupDir, args[0])
		if err != nil {
			return err
		}
		return r.stopDoStart(env, func() error {
			return r.step(env, driver.VerbRestore, archive)
		})
	case OpUninstall:
		return r.uninstall(env)
	default:
		return fmt.Errorf("unknown op %q", op)
	}
}

// uninstall stops the server if needed, has the driver tear down everything
// it created outside $SERVO_DATA_DIR, then removes the driver's data dir.
// Backups are deliberately kept. The server is never restarted afterwards.
func (r *Runner) uninstall(env driver.Env) error {
	r.say("checking server status")
	status, err := r.status(env)
	if err != nil {
		return fmt.Errorf("status probe failed: %w", err)
	}
	r.say("server is %s", status)
	if status == driver.StatusOnline {
		if err := r.step(env, driver.VerbStop); err != nil {
			return err
		}
	}

	if err := r.step(env, driver.VerbUninstall); err != nil {
		return err
	}

	r.say("removing driver data dir %s", env.DataDir)
	if err := r.setStep("remove-data"); err != nil {
		return err
	}
	if _, err := r.EnvFor(filepath.Base(env.DriverPath)); err != nil {
		return err
	}
	if err := os.RemoveAll(env.DataDir); err != nil {
		return fmt.Errorf("remove data dir: %w", err)
	}
	return nil
}

// stopDoStart probes server state, stops it if online, runs fn, and restores
// the prior state.
func (r *Runner) stopDoStart(env driver.Env, fn func() error) error {
	r.say("checking server status")
	status, err := r.status(env)
	if err != nil {
		return fmt.Errorf("status probe failed: %w", err)
	}
	r.say("server is %s", status)

	wasOnline := status == driver.StatusOnline
	if wasOnline {
		if err := r.step(env, driver.VerbStop); err != nil {
			return err
		}
	}

	if err := fn(); err != nil {
		return err
	}

	if wasOnline {
		return r.step(env, driver.VerbStart)
	}
	return nil
}

// Keep only the final stdout line without buffering an archive command's logs.
type lastLine struct{ data []byte }

func (b *lastLine) Write(p []byte) (int, error) {
	n := len(p)
	for _, c := range p {
		if len(b.data) > 0 && b.data[len(b.data)-1] == '\n' {
			b.data = b.data[:0]
		}
		b.data = append(b.data, c)
		if len(b.data) > 8192 {
			b.data = b.data[len(b.data)-8192:]
		}
	}
	return n, nil
}
func (b *lastLine) String() string { return string(b.data) }

func (r *Runner) prepareEnv(env driver.Env) error {
	l := layout.Layout{DriverData: r.paths.DataDir, Backups: r.paths.BackupsDir}
	_, _, err := l.EnsureDriver(filepath.Base(env.DriverPath))
	return err
}
