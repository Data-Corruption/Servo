//go:build !windows

package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the shipped script with fake container tools; this does not claim
// compatibility with a real Palworld image or host service manager.
func palworldFixture(t *testing.T, podman string) (run func(...string) (string, error), data, backups, tools string) {
	t.Helper()
	root := t.TempDir()
	data, backups, tools = filepath.Join(root, "data"), filepath.Join(root, "backups"), filepath.Join(root, "tools")
	for _, dir := range []string{data, backups, tools} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tools, "podman"), []byte("#!/bin/sh\n"+podman), 0700); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../drivers/fedora-palworld.sh")
	if err != nil {
		t.Fatal(err)
	}
	run = func(args ...string) (string, error) {
		cmd := exec.Command("sh", append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"), "SERVO_DATA_DIR="+data, "SERVO_BACKUP_DIR="+backups, "SERVO_VERSION=test")
		output, err := cmd.CombinedOutput()
		return string(output), err
	}
	return
}

func TestPalworldInspectionErrorsFailClosed(t *testing.T) {
	run, _, _, _ := palworldFixture(t, "echo inspection-failed >&2; exit 125\n")
	for _, verb := range []string{"status", "start", "stop"} {
		output, err := run(verb)
		if err == nil {
			t.Fatalf("%s succeeded: %s", verb, output)
		}
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == ExitStopped {
			t.Fatalf("%s reported offline: %s", verb, output)
		}
	}
}

func TestPalworldScopeFailureHasNoFallback(t *testing.T) {
	run, _, _, tools := palworldFixture(t, `case "$1" in container) exit 0 ;; inspect) echo false ;; start) echo UNSAFE-FALLBACK ;; esac`)
	if err := os.WriteFile(filepath.Join(tools, "systemd-run"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := run("start")
	if err == nil || !strings.Contains(output, "Scope creation failed") || strings.Contains(output, "UNSAFE-FALLBACK") {
		t.Fatalf("scope failure: %v %s", err, output)
	}
}

func TestPalworldBackupAndRestoreValidation(t *testing.T) {
	run, data, backups, _ := palworldFixture(t, "exit 125\n")
	saves := filepath.Join(data, "Pal", "Saved")
	if err := os.MkdirAll(saves, 0700); err != nil {
		t.Fatal(err)
	}
	world := filepath.Join(saves, "world.sav")
	if err := os.WriteFile(world, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := run("backup")
	if err != nil {
		t.Fatalf("backup: %v %s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	archive := lines[len(lines)-1]
	if filepath.Dir(archive) != backups {
		t.Fatalf("bad archive path: %q", archive)
	}
	if err := os.WriteFile(world, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(backups, "invalid.tar.gz")
	if err := os.WriteFile(invalid, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := run("restore", invalid); err == nil {
		t.Fatalf("invalid restore succeeded: %s", output)
	}
	if value, err := os.ReadFile(world); err != nil || string(value) != "current" {
		t.Fatalf("invalid restore touched saves: %q %v", value, err)
	}
	if output, err := run("restore", archive); err != nil {
		t.Fatalf("restore: %v %s", err, output)
	}
	if value, err := os.ReadFile(world); err != nil || string(value) != "original" {
		t.Fatalf("restored saves: %q %v", value, err)
	}
}
