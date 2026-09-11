//go:build !windows

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEveryFailedSequenceStopsWithoutRecovery(t *testing.T) {
	for _, tc := range []struct {
		name        string
		op          Op
		replacement string
		want        string
	}{
		{"status", OpUpdate, `status) exit 1 ;;`, "status"},
		{"stop", OpUpdate, `stop) exit 1 ;;`, "status stop"},
		{"update", OpUpdate, `update) exit 1 ;;`, "status stop update"},
		{"backup", OpBackup, `backup) exit 1 ;;`, "status stop backup"},
		{"restore", OpRestore, `restore) exit 1 ;;`, "status stop restore"},
		{"start", OpUpdate, `start) exit 1 ;;`, "status stop update start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, fixtureDriver)
			if err := os.WriteFile(filepath.Join(f.data, "online"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(f.backup, "fixture.tar.gz")
			if err := os.WriteFile(archive, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			// Inject the failing verb ahead of the normal case, after its call log.
			script := strings.Replace(fixtureDriver, `case "$1" in`+"\n", `case "$1" in`+"\n  "+tc.replacement+"\n", 1)
			if err := os.WriteFile(filepath.Join(f.drivers, "fixture.sh"), []byte("#!/bin/sh\n"+script), 0755); err != nil {
				t.Fatal(err)
			}
			args := []string{}
			if tc.op == OpRestore {
				args = []string{filepath.Base(archive)}
			}
			result := f.runOp(t, tc.op, args...)
			if result.Success || result.Outcome != "failed" {
				t.Fatalf("result: %+v", result)
			}
			if got := strings.Join(f.calls(t), " "); !strings.HasPrefix(got, tc.want) || strings.Count(got, "start") > strings.Count(tc.want, "start") {
				t.Fatalf("calls %q want %q", got, tc.want)
			}
		})
	}
}
func TestInvalidBackupDoesNotPruneOrRestart(t *testing.T) {
	script := strings.Replace(fixtureDriver, `backup)`, `backup) echo /tmp/outside.tar.gz; exit 0 ;; ignored)`, 1)
	f := newFixture(t, script)
	for _, name := range []string{"a.gz", "b.gz", "c.gz"} {
		if err := os.WriteFile(filepath.Join(f.backup, name), []byte("saved"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.runOp(t, OpStart)
	f.resetCalls()
	result := f.runOp(t, OpBackup)
	if result.Success {
		t.Fatal("accepted invalid backup path")
	}
	files, err := ListBackups(f.backup)
	if err != nil || len(files) != 3 {
		t.Fatalf("pruned after invalid output: %v %v", files, err)
	}
	if got := strings.Join(f.calls(t), " "); got != "status stop backup" {
		t.Fatal(got)
	}
}
func TestShutdownJoinsAndRecordsInterruptedOperation(t *testing.T) {
	f := newFixture(t, strings.Replace(fixtureDriver, `install) echo "installed" ;;`, `install) echo entered; sleep 30 ;;`, 1))
	done, err := f.runner.Start(OpInstall)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { data, _ := f.runner.Tail(0); return strings.Contains(string(data), "entered") })
	started := time.Now()
	if err := f.runner.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("shutdown did not promptly join driver")
	}
	<-done
	if got := f.runner.Status().Last; got == nil || got.Outcome != "interrupted" || got.Step != "install" {
		t.Fatalf("result: %+v", got)
	}
	if _, err := f.runner.Start(OpStart); !errors.Is(err, context.Canceled) {
		t.Fatalf("admission after close: %v", err)
	}
	next, err := New(context.Background(), f.runner.db, f.runner.log, f.runner.paths)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if got := next.Status().Last; got == nil || got.Outcome != "interrupted" {
		t.Fatalf("reload: %+v", got)
	}
}
func TestStartupMarksUnfinishedRecordWithoutExecuting(t *testing.T) {
	f := newFixture(t, fixtureDriver)
	_ = f.runner.Close()
	saved := Result{ID: "unfinished", Driver: "fixture.sh", Op: OpRestore, Step: "restore", Outcome: "running", StartedAt: time.Now()}
	data, _ := json.Marshal(saved)
	if _, err := f.runner.db.Exec(`INSERT INTO game_operation(id,value) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value`, string(data)); err != nil {
		t.Fatal(err)
	}
	next, err := New(context.Background(), f.runner.db, f.runner.log, f.runner.paths)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.Status().Last.Outcome != "interrupted" {
		t.Fatal("unfinished record not marked")
	}
	if _, err := os.Stat(filepath.Join(f.data, "calls")); !os.IsNotExist(err) {
		t.Fatal("startup executed driver")
	}
}
func TestActivationReservesAdmissionAndCancelsCleanly(t *testing.T) {
	script := strings.Replace(fixtureDriver, `deps) exit 0 ;;`, `deps) echo validating > "$SERVO_DATA_DIR/validating"; sleep 30 ;;`, 1)
	f := newFixture(t, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := f.runner.Activate(ctx, "fixture.sh"); result <- err }()
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(f.data, "validating")); return err == nil })
	if _, err := f.runner.Start(OpStart); !errors.Is(err, ErrBusy) {
		t.Fatalf("operation raced activation: %v", err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("activation did not cancel")
	}
	if f.runner.Busy() {
		t.Fatal("activation left admission reserved")
	}
}
func TestCachedReadsAndInteractiveProbePreemption(t *testing.T) {
	f := newFixture(t, strings.Replace(fixtureDriver, `status) [ -f "$SERVO_DATA_DIR/online" ] && exit 0 || exit 3 ;;`, `status) echo probing > "$SERVO_DATA_DIR/probing"; sleep 30 ;;`, 1))
	poller := NewPoller(f.runner)
	done := make(chan struct{})
	go func() { defer close(done); poller.Refresh() }()
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(f.data, "probing")); return err == nil })
	read := make(chan struct{})
	go func() { _ = poller.Game(context.Background()); close(read) }()
	select {
	case <-read:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("HTTP snapshot blocked behind driver")
	}
	start := time.Now()
	operation, err := f.runner.Start(OpStart)
	if err != nil {
		t.Fatal(err)
	}
	<-operation
	<-done
	if time.Since(start) > 2*time.Second {
		t.Fatal("interactive action did not preempt probe")
	}
	if !f.runner.Status().Last.Success {
		t.Fatal("start failed")
	}
}
func TestArchivePathsRejectLinksAndTraversal(t *testing.T) {
	f := newFixture(t, fixtureDriver)
	outside := filepath.Join(t.TempDir(), "private")
	_ = os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(f.backup, "linked.gz")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../private", outside, "linked.gz"} {
		if _, err := ResolveBackup(f.backup, name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestPollingResetsOffsetAcrossOperationsAndRestart(t *testing.T) {
	f := newFixture(t, fixtureDriver)
	first := f.runOp(t, OpInstall)
	_, _, offset := f.runner.Poll("", 0)
	second := f.runOp(t, OpStart)
	snapshot, output, next := f.runner.Poll(first.ID, offset)
	if snapshot.ID != second.ID || !strings.Contains(string(output), "done") || strings.Contains(string(output), "installed") {
		t.Fatalf("mixed operation logs: %+v %q", snapshot, output)
	}
	_, output, _ = f.runner.Poll(second.ID, next)
	if len(output) != 0 {
		t.Fatalf("repeated log output: %q", output)
	}
	if err := f.runner.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(context.Background(), f.runner.db, f.runner.log, f.runner.paths)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	snapshot, _, next = restarted.Poll(second.ID, next)
	if snapshot.ID != second.ID || snapshot.Last == nil || !snapshot.Last.Success || next != 0 {
		t.Fatalf("restart poll: %+v offset %d", snapshot, next)
	}
}

func TestInstallRequiresConfirmedStoppedGame(t *testing.T) {
	for _, tc := range []struct {
		name    string
		script  string
		online  bool
		success bool
		calls   string
	}{
		{"stopped", fixtureDriver, false, true, "status install"},
		{"running", fixtureDriver, true, false, "status"},
		{"probe failure", strings.Replace(fixtureDriver, `status) [ -f`, `status) exit 1 ;; unused) [ -f`, 1), false, false, "status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.script)
			if tc.online {
				if err := os.WriteFile(filepath.Join(f.data, "online"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := f.runOp(t, OpInstall)
			if result.Success != tc.success {
				t.Fatalf("install outcome: %+v", result)
			}
			if got := strings.Join(f.calls(t), " "); got != tc.calls {
				t.Fatalf("driver calls %q, want %q", got, tc.calls)
			}
			if !tc.success && (result.Outcome != "failed" || result.Step != "status") {
				t.Fatalf("failed install record: %+v", result)
			}
			if tc.online {
				if _, err := os.Stat(filepath.Join(f.data, "online")); err != nil {
					t.Fatalf("install disturbed running game: %v", err)
				}
			}
		})
	}
}
