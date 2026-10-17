//go:build !windows

package driver

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests exercise the shipped shell script, not a real container image.
// Fedora/SELinux, Steam connectivity and save-on-SIGINT need live validation.
type valheimFixture struct {
	t                     *testing.T
	root, data, backups   string
	tools, script, source string
}

func newValheimFixture(t *testing.T) *valheimFixture {
	t.Helper()
	f := &valheimFixture{t: t, root: t.TempDir()}
	f.data = filepath.Join(f.root, "data with spaces")
	f.backups = filepath.Join(f.root, "backups")
	f.tools = filepath.Join(f.root, "tools")
	f.script = filepath.Join(f.root, "valheim.sh")
	for _, dir := range []string{f.data, f.backups, f.tools, filepath.Join(f.data, "runtime"), filepath.Join(f.data, "saves")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile("../../drivers/fedora-valheim.sh")
	if err != nil {
		t.Fatal(err)
	}
	f.source = strings.ReplaceAll(string(source), `SERVER_PASSWORD=""`, `SERVER_PASSWORD="test password"`)
	f.source = strings.ReplaceAll(f.source, "START_READY_TIMEOUT=300", "START_READY_TIMEOUT=1")
	f.source = strings.ReplaceAll(f.source, "STOP_TIMEOUT=120", "STOP_TIMEOUT=1")
	f.write(f.script, f.source)
	f.write(filepath.Join(f.tools, "podman"), "#!/bin/sh\n"+valheimPodman)
	f.write(filepath.Join(f.tools, "systemd-run"), `#!/bin/sh
echo scope >> "$FIXTURE/trace"
[ ! -f "$FIXTURE/scope-fail" ] || exit 1
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
`)
	f.write(filepath.Join(f.tools, "systemctl"), "#!/bin/sh\n[ ! -f \"$FIXTURE/systemd-fail\" ]\n")
	f.write(filepath.Join(f.tools, "sleep"), "#!/bin/sh\nexit 0\n")
	f.write(filepath.Join(f.tools, "date"), `#!/bin/sh
case "$1" in
  +%s)
    n=0
    if [ -f "$FIXTURE/clock" ]; then read -r n < "$FIXTURE/clock"; fi
    n=$((n+1))
    echo "$n" > "$FIXTURE/clock"
    echo "$n"
    ;;
  *) echo 20260919-120000 ;;
esac
`)
	return f
}

func (f *valheimFixture) write(path, value string) {
	f.t.Helper()
	if err := os.WriteFile(path, []byte(value), 0700); err != nil {
		f.t.Fatal(err)
	}
}

func (f *valheimFixture) flag(name string) { f.write(filepath.Join(f.root, name), "yes") }

func (f *valheimFixture) configure(values map[string]string) {
	f.t.Helper()
	lines := strings.Split(f.source, "\n")
	for name, value := range values {
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, name+"=") {
				lines[i] = name + "=" + value
				found = true
				break
			}
		}
		if !found {
			f.t.Fatalf("unknown config field: %s", name)
		}
	}
	f.write(f.script, strings.Join(lines, "\n"))
}

func (f *valheimFixture) run(args ...string) (string, int) {
	f.t.Helper()
	cmd := exec.Command("sh", append([]string{f.script}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+f.tools+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FIXTURE="+f.root, "SERVO_DATA_DIR="+f.data, "SERVO_BACKUP_DIR="+f.backups, "SERVO_VERSION=test")
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return string(output), exit.ExitCode()
	}
	f.t.Fatal(err)
	return "", -1
}

func (f *valheimFixture) success(args ...string) string {
	f.t.Helper()
	out, code := f.run(args...)
	if code != 0 {
		f.t.Fatalf("%v: exit %d: %s", args, code, out)
	}
	return out
}

func (f *valheimFixture) failure(args ...string) string {
	f.t.Helper()
	out, code := f.run(args...)
	if code != 1 {
		f.t.Fatalf("%v: want exit 1, got %d: %s", args, code, out)
	}
	return out
}

func (f *valheimFixture) read(path string) string {
	f.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

const valheimPodman = `
printf '<%s>' "$@" >> "$FIXTURE/trace"
echo >> "$FIXTURE/trace"
case "$1" in
info)
  [ ! -f "$FIXTURE/rootful" ] || { echo false; exit; }
  echo true
  ;;
container)
  for last do :; done
  case "$last" in *updater*) kind=updater ;; *) kind=game ;; esac
  case "$2" in
    exists)
      [ ! -f "$FIXTURE/query-fail" ] || exit 125
      [ -f "$FIXTURE/$kind" ]
      ;;
    inspect)
      [ ! -f "$FIXTURE/inspect-fail" ] || exit 125
      case "$4" in
        *dev.servo.valheim.data*)
          if [ -f "$FIXTURE/wrong-owner-$kind" ]; then echo /unrelated; else echo "$SERVO_DATA_DIR"; fi ;;
        *dev.servo.valheim.role*) echo "$kind" ;;
        '{{.Id}}') echo "$kind-id" ;;
        '{{.State.Status}}') cat "$FIXTURE/$kind" ;;
        *) exit 125 ;;
      esac
      ;;
    *) exit 125 ;;
  esac
  ;;
start)
  echo running > "$FIXTURE/game"
  [ ! -f "$FIXTURE/log-fail" ] || { rm "$SERVO_DATA_DIR/runtime/valheim.log"; exit; }
  [ ! -f "$FIXTURE/early-exit" ] || { echo exited > "$FIXTURE/game"; exit; }
  [ ! -f "$FIXTURE/not-ready" ] || exit 0
  echo 'Game server connected' >> "$SERVO_DATA_DIR/runtime/valheim.log"
  ;;
kill)
  [ "$2" = --signal ] && [ "$3" = SIGINT ] || exit 125
  [ ! -f "$FIXTURE/stop-timeout" ] || exit 0
  echo exited > "$FIXTURE/game"
  ;;
pull)
  [ ! -f "$FIXTURE/pull-fail" ] || exit 125
  echo image-id
  ;;
image)
  case "$4" in
    '{{.Id}}') echo image-id ;;
    '{{json .RepoDigests}}') echo '["docker.io/cm2network/valheim@sha256:test-fixture"]' ;;
    *) exit 125 ;;
  esac
  ;;
run)
  if [ -f "$FIXTURE/hang-update" ]; then
    echo running > "$FIXTURE/updater"
    exec /bin/sleep 60
  fi
  echo exited > "$FIXTURE/updater"
  [ ! -f "$FIXTURE/update-fail" ] || { echo 'Steam download failed'; exit 1; }
  [ ! -f "$FIXTURE/no-success" ] || { echo 'Error despite exit zero'; exit; }
  mkdir -p "$SERVO_DATA_DIR/server/steamapps"
  if [ ! -f "$FIXTURE/no-binary" ]; then
    printf '#!/bin/sh\n' > "$SERVO_DATA_DIR/server/valheim_server.x86_64"
    chmod 700 "$SERVO_DATA_DIR/server/valheim_server.x86_64"
  fi
  build=200
  [ ! -f "$FIXTURE/public-build" ] || read -r build < "$FIXTURE/public-build"
  if [ ! -f "$FIXTURE/no-app-info" ]; then
    # Shaped like app_info_print: depot manifests and older branches come
    # with their own "public" and "buildid" keys.
    printf '\033[0mAppID : 896660, change number : 1/1\n'
    printf '"896660"\n{\n\t"depots"\n\t{\n\t\t"896661"\n\t\t{\n\t\t\t"manifests"\n\t\t\t{\n'
    printf '\t\t\t\t"public"\n\t\t\t\t{\n\t\t\t\t\t"gid"\t\t"1"\n\t\t\t\t}\n\t\t\t}\n\t\t}\n'
    printf '\t\t"branches"\n\t\t{\n\t\t\t"default_old"\n\t\t\t{\n\t\t\t\t"buildid"\t\t"100"\n\t\t\t}\n'
    printf '\t\t\t"public"\n\t\t\t{\n\t\t\t\t"buildid"\t\t"%s"\n\t\t\t}\n\t\t}\n\t}\n}\n' "$build"
  fi
  manifest="$SERVO_DATA_DIR/server/steamapps/appmanifest_896660.acf"
  if [ -f "$manifest" ]; then
    # Real SteamCMD trusted a stale manifest without comparing builds.
    echo "Success! App '896660' already up to date."
    exit
  fi
  [ ! -f "$FIXTURE/wrong-build" ] || build=100
  printf '"AppState"\n{\n\t"appid"\t\t"896660"\n\t"buildid"\t\t"%s"\n}\n' "$build" > "$manifest"
  echo "Success! App '896660' fully installed."
  ;;
rm)
  case "$2" in updater-id) rm "$FIXTURE/updater" ;; game-id) rm "$FIXTURE/game" ;; *) exit 125 ;; esac
  ;;
create)
  [ ! -f "$FIXTURE/create-fail" ] || exit 125
  echo created > "$FIXTURE/game"
  ;;
*) exit 125 ;;
esac
`

func TestValheimMetadataAndDependencies(t *testing.T) {
	f := newValheimFixture(t)
	f.write(f.script, strings.ReplaceAll(f.source, `SERVER_PASSWORD="test password"`, `SERVER_PASSWORD=""`))
	if err := os.RemoveAll(f.data); err != nil {
		t.Fatal(err)
	}
	info, err := ParseInfo([]byte(f.success("describe")))
	if err != nil || info.Game != "valheim" || !info.Containerized || strings.Join(info.Capabilities, ",") != "restore,uninstall" {
		t.Fatalf("describe: %+v %v", info, err)
	}
	f.success("deps")
	if _, err := os.Stat(f.data); !os.IsNotExist(err) {
		t.Fatalf("read-only probes created data: %v", err)
	}
	f.flag("rootful")
	f.failure("deps")
	if err := os.Remove(filepath.Join(f.root, "rootful")); err != nil {
		t.Fatal(err)
	}
	f.flag("systemd-fail")
	f.failure("deps")
	for _, verb := range []string{"notify", "players", "metrics", "version", "container-version", "unknown"} {
		if out, code := f.run(verb); code != 4 {
			t.Fatalf("%s: %d %s", verb, code, out)
		}
	}
}

func TestValheimStatusAndInspectionFailures(t *testing.T) {
	for _, state := range []string{"missing", "created", "exited", "running", "paused"} {
		t.Run(state, func(t *testing.T) {
			f := newValheimFixture(t)
			if state != "missing" {
				f.write(filepath.Join(f.root, "game"), state)
			}
			want := 3
			if state == "running" {
				want = 0
			} else if state == "paused" {
				want = 1
			}
			if out, code := f.run("status"); code != want {
				t.Fatalf("status: %d %s", code, out)
			}
		})
	}
	for _, flag := range []string{"query-fail", "inspect-fail", "wrong-owner-game", "wrong-owner-updater"} {
		t.Run(flag, func(t *testing.T) {
			f := newValheimFixture(t)
			f.write(filepath.Join(f.root, "game"), "exited")
			f.write(filepath.Join(f.root, "updater"), "exited")
			f.flag(flag)
			verbs := []string{"start", "install", "update", "backup", "restore", "uninstall"}
			if flag != "wrong-owner-updater" {
				verbs = append(verbs, "status", "stop")
			}
			for _, verb := range verbs {
				f.failure(verb)
			}
			trace := f.read(filepath.Join(f.root, "trace"))
			for _, mutation := range []string{"<rm>", "<create>", "<run>", "<kill>", "<start>", "<pull>"} {
				if strings.Contains(trace, mutation) {
					t.Fatalf("mutated after inspection failure: %s", trace)
				}
			}
		})
	}
}

func TestValheimStartReadinessAndScope(t *testing.T) {
	for _, flag := range []string{"", "scope-fail", "not-ready", "early-exit", "log-fail"} {
		t.Run(flag, func(t *testing.T) {
			f := newValheimFixture(t)
			f.write(filepath.Join(f.root, "game"), "created")
			log := filepath.Join(f.data, "runtime", "valheim.log")
			if err := os.WriteFile(log, []byte("Game server connected (previous launch)\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if flag != "" {
				f.flag(flag)
				out := f.failure("start")
				if !strings.Contains(out, log) {
					t.Fatalf("failure did not identify game log: %s", out)
				}
			} else {
				f.success("start")
				firstLog := f.read(log)
				f.success("start")
				if strings.Contains(firstLog, "previous launch") || f.read(log) != firstLog {
					t.Fatal("Start reused stale readiness or cleared the running game's log")
				}
			}
			trace := f.read(filepath.Join(f.root, "trace"))
			if strings.Count(trace, "scope\n") != 1 || (flag == "scope-fail" && strings.Contains(trace, "<start>")) {
				t.Fatalf("scope or idempotence failure: %s", trace)
			}
			if strings.Contains(trace, "<pull>") || strings.Contains(trace, "<run>") {
				t.Fatalf("start performed an update: %s", trace)
			}
		})
	}
}

func TestValheimStartRejectsUnsafeLog(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			f := newValheimFixture(t)
			f.write(filepath.Join(f.root, "game"), "created")
			log := filepath.Join(f.data, "runtime", "valheim.log")
			outside := filepath.Join(f.root, "preserve")
			f.write(outside, "untouched")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(outside, log)
			case "directory":
				err = os.Mkdir(log, 0700)
			case "permissions":
				err = os.WriteFile(log, []byte("old log"), 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			f.failure("start")
			if f.read(outside) != "untouched" || strings.Contains(f.read(filepath.Join(f.root, "trace")), "<start>") {
				t.Fatal("unsafe log was modified or game was started")
			}
		})
	}
}

func TestValheimStop(t *testing.T) {
	f := newValheimFixture(t)
	f.success("stop")
	f.write(filepath.Join(f.root, "game"), "running")
	f.success("stop")
	f.success("stop")
	trace := f.read(filepath.Join(f.root, "trace"))
	if strings.Count(trace, "<kill><--signal><SIGINT>") != 1 {
		t.Fatalf("stop was not graceful/idempotent: %s", trace)
	}
	f.write(filepath.Join(f.root, "game"), "running")
	f.flag("stop-timeout")
	f.failure("stop")
	if got := f.read(filepath.Join(f.root, "game")); strings.TrimSpace(got) != "running" {
		t.Fatalf("force-killed game: %s", got)
	}
	for _, verb := range []string{"install", "update", "backup", "restore", "uninstall"} {
		f.failure(verb)
	}
}

func TestValheimInstallUpdateAndUninstall(t *testing.T) {
	f := newValheimFixture(t)
	if out := f.success("install"); !strings.Contains(out, "Valheim build: none -> 200") {
		t.Fatalf("install did not report the build: %s", out)
	}
	f.write(filepath.Join(f.root, "public-build"), "300")
	out := f.success("update") // Mock returns the same image ID; Steam must still run.
	if !strings.Contains(out, "Valheim build: 200 -> 300") {
		t.Fatalf("update did not report the build change: %s", out)
	}
	if strings.Contains(out, `"branches"`) || !strings.Contains(out, "Success! App '896660' fully installed.") {
		t.Fatalf("app info dump not filtered from SteamCMD output: %s", out)
	}
	if !strings.Contains(f.read(filepath.Join(f.data, "runtime", "steamcmd.log")), `"branches"`) {
		t.Fatal("steamcmd.log lost the app info dump")
	}
	trace := f.read(filepath.Join(f.root, "trace"))
	if strings.Count(trace, "<run>") != 2 || strings.Count(trace, "<create>") != 2 {
		t.Fatalf("unchanged image skipped Steam update: %s", trace)
	}
	// Update also validates: each updater lacks the old depot manifest.
	if strings.Count(trace, "<+app_info_print><896660><+app_update><896660><validate><+quit>") != 2 {
		t.Fatalf("install and update did not both validate: %s", trace)
	}
	for _, arg := range []string{
		"<+force_install_dir></data/server><+login><anonymous><+app_info_update><1><+app_info_print><896660>",
		"<--timeout><3300>", "<--userns=keep-id:uid=1000,gid=1000>", "<--user><1000:1000>",
		"<--restart=no>", "<2456:2456/udp>", "<2457:2457/udp>", "<-public><0>",
		"<--entrypoint></usr/bin/tini>", "<" + f.data + ":/data:Z>",
		"-logFile /data/runtime/valheim.log",
		"<-saveinterval><1800><-backups><4><-backupshort><7200><-backuplong><43200>",
		"<-setkey><playerevents>",
	} {
		if !strings.Contains(trace, arg) {
			t.Errorf("missing %s in %s", arg, trace)
		}
	}
	if strings.Contains(trace, "<start>") {
		t.Fatal("install/update started the game")
	}
	if strings.Contains(trace, "-logFile /dev/stdout") {
		t.Fatal("Unity was configured to open /dev/stdout as a log file")
	}
	for _, omitted := range []string{"<-preset>", "<-modifier>", "<-setkey><nobuildcost>", "<-setkey><passivemobs>", "<-setkey><nomap>"} {
		if strings.Contains(trace, omitted) {
			t.Errorf("default unexpectedly overrides world settings with %s", omitted)
		}
	}
	if _, err := os.Stat(filepath.Join(f.data, "runtime", "update-pending")); !os.IsNotExist(err) {
		t.Fatalf("pending marker not cleared: %v", err)
	}
	if digest := f.read(filepath.Join(f.data, "runtime", "image-digest")); !strings.Contains(digest, "sha256:test-fixture") {
		t.Fatalf("missing image digest: %s", digest)
	}
	f.write(filepath.Join(f.backups, "keep.tar.gz"), "archive")
	f.success("uninstall")
	f.success("uninstall")
	if got := f.read(filepath.Join(f.backups, "keep.tar.gz")); got != "archive" {
		t.Fatal("uninstall removed backups")
	}
	if strings.Contains(f.read(filepath.Join(f.root, "trace")), "<rmi>") {
		t.Fatal("uninstall removed a shared image")
	}
}

func TestValheimConfiguredLaunchOptions(t *testing.T) {
	f := newValheimFixture(t)
	f.configure(map[string]string{
		"WORLD_PRESET":          `"casual"`,
		"COMBAT":                `"hard"`,
		"DEATH_PENALTY":         `"easy"`,
		"RESOURCE_RATE":         `"most"`,
		"RAID_FREQUENCY":        `"none"`,
		"PORTALS":               `"casual"`,
		"NO_BUILD_COST":         "true",
		"PASSIVE_ENEMIES":       "true",
		"NO_MAP":                "true",
		"SAVE_INTERVAL":         "900",
		"WORLD_BACKUPS":         "0",
		"BACKUP_SHORT_INTERVAL": "3600",
		"BACKUP_LONG_INTERVAL":  "21600",
		"SERVER_NAME":           `'Friends; $(do-not-execute) *'`,
		"SERVER_PASSWORD":       `'secret with spaces; $dollars *'`,
	})
	f.success("install")
	trace := f.read(filepath.Join(f.root, "trace"))
	for _, sequence := range []string{
		"<-preset><casual><-modifier><combat><hard><-modifier><deathpenalty><easy><-modifier><resources><most><-modifier><raids><none><-modifier><portals><casual><-setkey><playerevents><-setkey><nobuildcost><-setkey><passivemobs><-setkey><nomap>",
		"<-saveinterval><900><-backups><0><-backupshort><3600><-backuplong><21600>",
		"<-name><Friends; $(do-not-execute) *>",
		"<-password><secret with spaces; $dollars *>",
	} {
		if !strings.Contains(trace, sequence) {
			t.Errorf("missing argv sequence %s in %s", sequence, trace)
		}
	}
}

func TestValheimPlayerRaidsCanBeOmitted(t *testing.T) {
	f := newValheimFixture(t)
	f.configure(map[string]string{"PLAYER_BASED_RAIDS": "false"})
	f.success("install")
	if trace := f.read(filepath.Join(f.root, "trace")); strings.Contains(trace, "<-setkey>") {
		t.Fatalf("false switch emitted a key: %s", trace)
	}
}

func TestValheimInvalidLaunchOptionsFailBeforeMutation(t *testing.T) {
	for name, value := range map[string]string{
		"WORLD_PRESET":          `"custom"`,
		"COMBAT":                `"normal"`,
		"DEATH_PENALTY":         `"none"`,
		"RESOURCE_RATE":         `"2"`,
		"RAID_FREQUENCY":        `"sometimes"`,
		"PORTALS":               `'casual -setkey nomap'`,
		"PLAYER_BASED_RAIDS":    "1",
		"NO_BUILD_COST":         "TRUE",
		"PASSIVE_ENEMIES":       "yes",
		"NO_MAP":                `""`,
		"SAVE_INTERVAL":         "0",
		"WORLD_BACKUPS":         "-1",
		"BACKUP_SHORT_INTERVAL": "0900",
		"BACKUP_LONG_INTERVAL":  "2147483648",
	} {
		t.Run(name, func(t *testing.T) {
			f := newValheimFixture(t)
			f.configure(map[string]string{name: value})
			for _, verb := range []string{"install", "update"} {
				if out := f.failure(verb); !strings.Contains(out, name) {
					t.Errorf("error did not identify %s: %s", name, out)
				}
			}
			if _, err := os.Stat(filepath.Join(f.root, "trace")); !os.IsNotExist(err) {
				t.Fatalf("invalid config reached Podman: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.data, "runtime", "update-pending")); !os.IsNotExist(err) {
				t.Fatalf("invalid config changed installation: %v", err)
			}
		})
	}
}

func TestValheimFailedUpdatesBlockStart(t *testing.T) {
	for _, flag := range []string{"pull-fail", "update-fail", "no-success", "no-binary", "no-app-info", "wrong-build", "create-fail"} {
		t.Run(flag, func(t *testing.T) {
			f := newValheimFixture(t)
			f.write(filepath.Join(f.root, "game"), "exited")
			f.flag(flag)
			f.failure("update")
			if out := f.failure("start"); !strings.Contains(out, "incomplete install/update") {
				t.Fatalf("missing failure marker: %s", out)
			}
			if err := os.Remove(filepath.Join(f.root, flag)); err != nil {
				t.Fatal(err)
			}
			f.success("update")
			f.success("start")
		})
	}
}

// SteamCMD has reported success for an outdated install, which left clients
// with an incompatible version after Update.
func TestValheimRejectsOutdatedBuild(t *testing.T) {
	f := newValheimFixture(t)
	f.success("install")
	f.write(filepath.Join(f.root, "public-build"), "300")
	f.flag("wrong-build")
	if out := f.failure("update"); !strings.Contains(out, "installed build 100 is not the current public build 300") {
		t.Fatalf("outdated build was accepted: %s", out)
	}
	if out := f.failure("start"); !strings.Contains(out, "incomplete install/update") {
		t.Fatalf("outdated install can start: %s", out)
	}
	if err := os.Remove(filepath.Join(f.root, "wrong-build")); err != nil {
		t.Fatal(err)
	}
	if out := f.success("update"); !strings.Contains(out, "Valheim build: 100 -> 300") {
		t.Fatalf("retry did not update: %s", out)
	}
	f.success("start")
}

func TestValheimInterruptedUpdaterPreventsOverlap(t *testing.T) {
	f := newValheimFixture(t)
	f.success("install")
	if err := os.WriteFile(filepath.Join(f.data, "runtime", "update-pending"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	f.write(filepath.Join(f.root, "updater"), "running")
	for _, verb := range []string{"start", "install", "update", "backup", "restore", "uninstall"} {
		f.failure(verb)
	}
	f.write(filepath.Join(f.root, "updater"), "exited")
	f.failure("start") // Updater exit alone does not authorize starting.
	f.success("update")
	f.success("start")
}

func TestValheimCancellationKeepsPendingMarker(t *testing.T) {
	f := newValheimFixture(t)
	f.flag("hang-update")
	t.Setenv("PATH", f.tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FIXTURE", f.root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, Env{DriverPath: f.script, DataDir: f.data, BackupDir: f.backups}, io.Discard, VerbUpdate)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(f.root, "updater")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("updater did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("driver did not stop promptly")
	}
	if out := f.failure("start"); !strings.Contains(out, "incomplete install/update") {
		t.Fatalf("cancellation lost pending marker: %s", out)
	}
	f.failure("update") // Durable updater state still prevents overlapping work.
}

func TestValheimPrivatePathsAndPassword(t *testing.T) {
	f := newValheimFixture(t)
	f.write(f.script, strings.ReplaceAll(f.source, `SERVER_PASSWORD="test password"`, `SERVER_PASSWORD=""`))
	f.failure("install")
	f.write(f.script, f.source)
	if err := os.Chmod(f.data, 0755); err != nil {
		t.Fatal(err)
	}
	f.failure("install")
	info, err := os.Stat(f.data)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("silently repaired permissions: %v %v", info, err)
	}
	if err := os.Chmod(f.data, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(f.root, "outside")
	f.write(outside, "preserve")
	if err := os.Symlink(outside, filepath.Join(f.data, "runtime", "steamcmd.log")); err != nil {
		t.Fatal(err)
	}
	f.failure("install")
	if got := f.read(outside); got != "preserve" {
		t.Fatal("overwrote symlink target")
	}
	if err := os.Remove(filepath.Join(f.data, "runtime", "steamcmd.log")); err != nil {
		t.Fatal(err)
	}
	steamapps := filepath.Join(f.data, "server", "steamapps")
	if err := os.MkdirAll(steamapps, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(steamapps, "appmanifest_896660.acf")); err != nil {
		t.Fatal(err)
	}
	if out := f.failure("install"); !strings.Contains(out, "symlink") {
		t.Fatalf("accepted a symlinked Steam manifest: %s", out)
	}
	if got := f.read(outside); got != "preserve" {
		t.Fatal("overwrote manifest symlink target")
	}
}

func TestValheimBackupRestore(t *testing.T) {
	f := newValheimFixture(t)
	world := filepath.Join(f.data, "saves", "world.db")
	f.write(world, "original")
	f.write(filepath.Join(f.data, "saves", "adminlist.txt"), "Steam_123")
	out := f.success("backup")
	archive := strings.TrimSpace(out)
	if !filepath.IsAbs(archive) || filepath.Dir(archive) != f.backups {
		t.Fatalf("bad archive path: %q", archive)
	}
	f.write(world, "changed")
	f.success("restore", archive)
	if got := f.read(world); got != "original" {
		t.Fatalf("restored wrong data: %s", got)
	}
	if got := f.read(filepath.Join(f.data, "saves", "adminlist.txt")); got != "Steam_123" {
		t.Fatalf("lost permission lists: %s", got)
	}
	// A compression failure must never leave a completed-looking backup.
	f.write(filepath.Join(f.tools, "gzip"), "#!/bin/sh\nexit 1\n")
	if output, code := f.run("backup"); code == 0 {
		t.Fatalf("compression failure succeeded: %s", output)
	}
	entries, err := os.ReadDir(f.backups)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(archive) {
		t.Fatalf("partial archive or staging left behind: %v %v", entries, err)
	}
}

func TestValheimRejectsUnsafeArchives(t *testing.T) {
	cases := []struct {
		name, path string
		kind       byte
	}{
		{"traversal", "saves/../../escaped", tar.TypeReg},
		{"absolute", "/tmp/escaped", tar.TypeReg},
		{"wrong-root", "server/binary", tar.TypeReg},
		{"symlink", "saves/link", tar.TypeSymlink},
		{"hardlink", "saves/link", tar.TypeLink},
		{"device", "saves/device", tar.TypeChar},
		{"fifo", "saves/fifo", tar.TypeFifo},
		{"newline", "saves/world\nname", tar.TypeReg},
		{"backslash", "saves/world\\name", tar.TypeReg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newValheimFixture(t)
			world := filepath.Join(f.data, "saves", "world.db")
			f.write(world, "current")
			archive := filepath.Join(f.backups, "unsafe.tar.gz")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(file)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: tc.path, Typeflag: tc.kind, Mode: 0600}
			if tc.kind == tar.TypeSymlink || tc.kind == tar.TypeLink {
				h.Linkname = "../../escaped"
			}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			for _, close := range []func() error{tw.Close, gz.Close, file.Close} {
				if err := close(); err != nil {
					t.Fatal(err)
				}
			}
			if out, code := f.run("restore", archive); code == 0 {
				t.Fatalf("unsafe archive succeeded: %s", out)
			}
			if got := f.read(world); got != "current" {
				t.Fatalf("unsafe archive changed saves: %s", got)
			}
		})
	}
}

func TestValheimBackupRejectsUnrestorableSaves(t *testing.T) {
	for _, kind := range []string{"symlink", "newline", "backslash"} {
		t.Run(kind, func(t *testing.T) {
			f := newValheimFixture(t)
			saves := filepath.Join(f.data, "saves")
			switch kind {
			case "symlink":
				if err := os.Symlink(f.root, filepath.Join(saves, "outside")); err != nil {
					t.Fatal(err)
				}
			case "newline":
				f.write(filepath.Join(saves, "world\nname"), "save")
			case "backslash":
				f.write(filepath.Join(saves, "world\\name"), "save")
			}
			f.failure("backup")
			entries, err := os.ReadDir(f.backups)
			if err != nil || len(entries) != 0 {
				t.Fatalf("published unrestorable archive: %v %v", entries, err)
			}
		})
	}
}

func TestValheimCorruptArchivePreservesSaves(t *testing.T) {
	f := newValheimFixture(t)
	world := filepath.Join(f.data, "saves", "world.db")
	f.write(world, "preserve")
	archive := filepath.Join(f.backups, "corrupt.tar.gz")
	f.write(archive, "invalid gzip")
	f.failure("restore", archive)
	if got := f.read(world); got != "preserve" {
		t.Fatalf("invalid archive replaced saves: %s", got)
	}
}
