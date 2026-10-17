#!/bin/sh
# Servo driver: vanilla Valheim for Steam clients, rootless Podman on Fedora x86-64.
# Setup, requirements and validation: fedora-valheim.md alongside this script.

set -u
umask 077

# --- config (edit me) ----------------------------------------------------
IMAGE="docker.io/cm2network/valheim:base"
CONTAINER="servo-valheim"
SERVER_NAME="Servo Valheim"
WORLD_NAME="Dedicated"
SERVER_PASSWORD=""              # required; at least five characters
GAME_PORT=2456
PUBLIC=0                        # 0 = direct connections only; 1 = listed
START_READY_TIMEOUT=300
STOP_TIMEOUT=120

# World modifiers. Empty = do not override the world's existing setting.
# Presets reset previous modifiers; individual choices below apply afterwards.
WORLD_PRESET=""                 # normal | casual | easy | hard | hardcore | immersive | hammer
COMBAT=""                       # veryeasy | easy | hard | veryhard
DEATH_PENALTY=""                # casual | veryeasy | easy | hard | hardcore
RESOURCE_RATE=""                # muchless (0.5x) | less (0.75x) | more (1.5x) | muchmore (2x) | most (3x)
RAID_FREQUENCY=""               # none (disable raids) | muchless | less | more | muchmore
PORTALS=""                      # casual (all items) | hard (no boss portals) | veryhard (no portals)

# true adds a world key; false omits it, not an explicit reset of a saved key.
# See fedora-valheim.md for resetting existing world modifiers.
PLAYER_BASED_RAIDS=true
NO_BUILD_COST=false
PASSIVE_ENEMIES=false
NO_MAP=false

# Valheim's own autosaves/backups, separate from Servo's stopped-game archives.
SAVE_INTERVAL=1800              # seconds between world saves (30 minutes)
WORLD_BACKUPS=4                 # built-in backups to keep; 0 disables them
BACKUP_SHORT_INTERVAL=7200      # seconds (2 hours)
BACKUP_LONG_INTERVAL=43200      # seconds (12 hours)

# --- helpers ------------------------------------------------------------
fail() { echo "$*" >&2; exit 1; }

need() {
  missing=0
  for tool in "$@"; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      echo "$tool"
      missing=1
    fi
  done
  return "$missing"
}

private_dir() {
  [ ! -L "$1" ] && [ -d "$1" ] || fail "not a regular directory: $1"
  mode=$(stat -c '%a:%u' "$1") || fail "cannot inspect directory: $1"
  [ "$mode" = "700:$(id -u)" ] || fail "directory must be owned by this user with mode 700: $1"
}

make_private_dir() {
  if [ ! -e "$1" ] && [ ! -L "$1" ]; then
    mkdir "$1" || fail "cannot create directory: $1"
  fi
  private_dir "$1"
}

private_file_if_present() {
  [ ! -L "$1" ] || fail "symlink where a file is required: $1"
  if [ -e "$1" ]; then
    [ -f "$1" ] || fail "not a regular file: $1"
    mode=$(stat -c '%a:%u' "$1") || fail "cannot inspect file: $1"
    [ "$mode" = "600:$(id -u)" ] || fail "file must be owned by this user with mode 600: $1"
  fi
}

validate_archive() {
  # Escape quoting makes embedded newlines/backslashes visible; reject those
  # names too rather than interpreting a line-oriented listing ambiguously.
  LC_ALL=C tar --quoting-style=escape -tzf "$1" > "$2/members" || exit 1
  awk 'BEGIN { count=0 } { count++; if ($0 !~ /^saves(\/|$)/ || $0 ~ /(^|\/)\.\.(\/|$)/ || index($0,"\\")) bad=1 } END { exit (bad || !count) }' "$2/members" || fail "archive contains unsafe paths"
  LC_ALL=C tar --quoting-style=escape -tvzf "$1" > "$2/types" || exit 1
  awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" { bad=1 } END { exit bad }' "$2/types" || fail "archive contains links or special files"
}

choice() {
  option=$1
  value=$2
  shift 2
  for allowed in "$@"; do
    [ "$value" != "$allowed" ] || return 0
  done
  fail "invalid $option; see the config block for allowed values"
}

integer() {
  case "$2" in ''|*[!0-9]*|0[0-9]*) fail "$1 must be a decimal integer without leading zeros" ;; esac
  [ "${#2}" -le 10 ] && [ "$2" -ge "$3" ] && [ "$2" -le 2147483647 ] || fail "$1 is out of range ($3..2147483647)"
}

validate_config() {
  [ "${#SERVER_PASSWORD}" -ge 5 ] || fail "set SERVER_PASSWORD to at least five characters"
  [ -n "$SERVER_NAME" ] && [ -n "$WORLD_NAME" ] || fail "server and world names must not be empty"
  case "$WORLD_NAME" in */*|.|..) fail "WORLD_NAME must be a name, not a path" ;; esac
  case "$PUBLIC" in 0|1) ;; *) fail "PUBLIC must be 0 or 1" ;; esac
  for number in "$GAME_PORT" "$START_READY_TIMEOUT" "$STOP_TIMEOUT"; do
    case "$number" in ''|*[!0-9]*|0*) fail "ports and timeouts must be positive decimal integers without leading zeros" ;; esac
    [ "${#number}" -le 5 ] || fail "port or timeout is out of range"
  done
  [ "$GAME_PORT" -gt 1024 ] && [ "$GAME_PORT" -lt 65535 ] || fail "GAME_PORT must be between 1025 and 65534"
  [ "$START_READY_TIMEOUT" -lt 540 ] && [ "$STOP_TIMEOUT" -lt 540 ] || fail "timeouts must be below 540 seconds"
  choice WORLD_PRESET "$WORLD_PRESET" "" normal casual easy hard hardcore immersive hammer
  choice COMBAT "$COMBAT" "" veryeasy easy hard veryhard
  choice DEATH_PENALTY "$DEATH_PENALTY" "" casual veryeasy easy hard hardcore
  choice RESOURCE_RATE "$RESOURCE_RATE" "" muchless less more muchmore most
  choice RAID_FREQUENCY "$RAID_FREQUENCY" "" none muchless less more muchmore
  choice PORTALS "$PORTALS" "" casual hard veryhard
  choice PLAYER_BASED_RAIDS "$PLAYER_BASED_RAIDS" true false
  choice NO_BUILD_COST "$NO_BUILD_COST" true false
  choice PASSIVE_ENEMIES "$PASSIVE_ENEMIES" true false
  choice NO_MAP "$NO_MAP" true false
  integer SAVE_INTERVAL "$SAVE_INTERVAL" 1
  integer WORLD_BACKUPS "$WORLD_BACKUPS" 0
  integer BACKUP_SHORT_INTERVAL "$BACKUP_SHORT_INTERVAL" 1
  integer BACKUP_LONG_INTERVAL "$BACKUP_LONG_INTERVAL" 1
}

# Sets STATE and CID. Missing is different from an inspection failure. Use the
# verified ID for subsequent mutations so a name replacement cannot be targeted.
inspect_owned() {
  STATE=missing
  CID=
  exists=0
  podman container exists "$1" || exists=$?
  case "$exists" in 0) ;; 1) return ;; *) fail "container query failed: $1" ;; esac
  CID=$(podman container inspect --format '{{.Id}}' "$1") || fail "ID inspection failed: $1"
  [ -n "$CID" ] || fail "empty container ID: $1"
  owner=$(podman container inspect --format '{{index .Config.Labels "dev.servo.valheim.data"}}' "$CID") || fail "owner inspection failed: $1"
  role=$(podman container inspect --format '{{index .Config.Labels "dev.servo.valheim.role"}}' "$CID") || fail "role inspection failed: $1"
  [ "$owner" = "$DATA" ] && [ "$role" = "$2" ] || fail "container name belongs to another installation: $1"
  STATE=$(podman container inspect --format '{{.State.Status}}' "$CID") || fail "state inspection failed: $1"
  case "$STATE" in running|created|configured|exited|stopped) ;; *) fail "unexpected container state: $STATE" ;; esac
}

require_stopped() {
  inspect_owned "$CONTAINER" game
  [ "$STATE" != running ] || fail "stop Valheim before this operation"
  inspect_owned "$UPDATER" updater
  [ "$STATE" != running ] || fail "an updater is still running; wait for it to exit before retrying"
}

create_game() {
  set -- -name "$SERVER_NAME" -world "$WORLD_NAME" \
    -password "$SERVER_PASSWORD" -port "$GAME_PORT" -public "$PUBLIC" \
    -saveinterval "$SAVE_INTERVAL" -backups "$WORLD_BACKUPS" \
    -backupshort "$BACKUP_SHORT_INTERVAL" -backuplong "$BACKUP_LONG_INTERVAL"
  # Valheim applies presets before custom modifiers and checkbox keys.
  [ -z "$WORLD_PRESET" ] || set -- "$@" -preset "$WORLD_PRESET"
  [ -z "$COMBAT" ] || set -- "$@" -modifier combat "$COMBAT"
  [ -z "$DEATH_PENALTY" ] || set -- "$@" -modifier deathpenalty "$DEATH_PENALTY"
  [ -z "$RESOURCE_RATE" ] || set -- "$@" -modifier resources "$RESOURCE_RATE"
  [ -z "$RAID_FREQUENCY" ] || set -- "$@" -modifier raids "$RAID_FREQUENCY"
  [ -z "$PORTALS" ] || set -- "$@" -modifier portals "$PORTALS"
  [ "$PLAYER_BASED_RAIDS" != true ] || set -- "$@" -setkey playerevents
  [ "$NO_BUILD_COST" != true ] || set -- "$@" -setkey nobuildcost
  [ "$PASSIVE_ENEMIES" != true ] || set -- "$@" -setkey passivemobs
  [ "$NO_MAP" != true ] || set -- "$@" -setkey nomap
  # Arguments are passed as argv, never interpolated into shell source. Tini
  # forwards SIGINT to the game, whose exec replaces the short launch shell.
  podman create --name "$CONTAINER" \
    --label "dev.servo.valheim.data=$DATA" --label dev.servo.valheim.role=game \
    --userns=keep-id:uid=1000,gid=1000 --user 1000:1000 --umask 077 \
    --restart=no --stop-signal SIGINT --stop-timeout "$STOP_TIMEOUT" \
    --log-driver k8s-file --log-opt max-size=10mb \
    -v "$DATA:/data:Z" \
    -p "$GAME_PORT:$GAME_PORT/udp" -p "$((GAME_PORT + 1)):$((GAME_PORT + 1))/udp" \
    --workdir /data/server --entrypoint /usr/bin/tini \
    -e SteamAppId=892970 -e LD_LIBRARY_PATH=/data/server/linux64 \
    "$image_id" -g -- /bin/sh -c '
      umask 077
      exec ./valheim_server.x86_64 -nographics -batchmode -logFile /data/runtime/valheim.log -savedir /data/saves "$@"
    ' servo-valheim "$@"
}

wait_ready() {
  deadline=$(( $(date +%s) + START_READY_TIMEOUT ))
  while :; do
    inspect_owned "$CONTAINER" game
    [ "$CID" = "$game_id" ] && [ "$STATE" = running ] || start_failed "Valheim exited before becoming ready"
    # Start clears this regular file before launch, so old readiness messages
    # cannot count. Do not print log contents: they can contain player details.
    private_file_if_present "$GAME_LOG"
    ready=0
    grep -F 'Game server connected' "$GAME_LOG" >/dev/null || ready=$?
    case "$ready" in
      0) echo "Valheim ready"; return ;;
      1) ;;
      *) start_failed "cannot read startup log" ;;
    esac
    [ "$(date +%s)" -lt "$deadline" ] || start_failed "Valheim did not become ready within ${START_READY_TIMEOUT}s"
    sleep 2
  done
}

start_failed() {
  printf 'Game log: %s\nContainer output: podman logs --tail 200 %s\n' "$GAME_LOG" "$CONTAINER" >&2
  fail "$@"
}

# Prints the installed Steam build, or nothing before the first install.
installed_build() {
  [ ! -L "$MANIFEST" ] || fail "symlink where a file is required: $MANIFEST"
  [ -e "$MANIFEST" ] || return 0
  [ -f "$MANIFEST" ] || fail "not a regular file: $MANIFEST"
  awk '$1 == "\"buildid\"" { gsub(/"/, "", $2); print $2; exit }' "$MANIFEST" || fail "cannot read Steam app manifest: $MANIFEST"
}

# Prints the public build from SteamCMD's app info dump. Depot manifests have
# "public" keys too; only the branch under "branches" names the build.
public_build() {
  awk '$1 == "\"branches\"" { b = 1; next }
    b && $1 == "\"public\"" { p = 1; next }
    p && $1 == "}" { exit }
    p && $1 == "\"buildid\"" { gsub(/"/, "", $2); print $2; exit }' "$DATA/runtime/steamcmd.log" || fail "cannot read SteamCMD log"
}

# describe/deps must also work before Servo has created any data directories.
case "${1:-}" in
describe)
  printf '%s\n' 'DRIVER_API=1' 'NAME=Valheim (Podman, Fedora)' 'GAME=valheim' \
    'CONTAINERIZED=true' 'CAPABILITIES=restore,uninstall'
  exit 0
  ;;
deps)
  need podman systemd-run systemctl tar gzip awk grep stat id uname date sleep \
    mkdir mktemp mv rm cat find || exit 1
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail "requires Linux x86-64"
  rootless=$(podman info --format '{{.Host.Security.Rootless}}') || fail "Podman is unavailable"
  [ "$rootless" = true ] || fail "Podman must run rootless as the Servo user"
  systemctl --user show-environment >/dev/null || fail "systemd user manager is unavailable"
  exit 0
  ;;
status|start|stop|install|update|backup|restore|uninstall) ;;
*) echo "unsupported verb: ${1:-<none>}" >&2; exit 4 ;;
esac

DATA=${SERVO_DATA_DIR:-}
case "$DATA" in /*) ;; *) fail "SERVO_DATA_DIR must be absolute" ;; esac
[ "$DATA" != / ] || fail "SERVO_DATA_DIR must not be the filesystem root"
UPDATER="$CONTAINER-updater"
GAME_LOG="$DATA/runtime/valheim.log"
MANIFEST="$DATA/server/steamapps/appmanifest_896660.acf"
if [ -e "$DATA" ] || [ -L "$DATA" ]; then private_dir "$DATA"; fi

case "$1" in
status)
  inspect_owned "$CONTAINER" game
  [ "$STATE" = running ] && exit 0
  exit 3
  ;;

start)
  private_dir "$DATA"
  private_dir "$DATA/runtime"
  [ ! -e "$DATA/runtime/update-pending" ] && [ ! -L "$DATA/runtime/update-pending" ] || fail "incomplete install/update; rerun Install or Update before Start"
  inspect_owned "$UPDATER" updater
  [ "$STATE" != running ] || fail "an updater is still running"
  inspect_owned "$CONTAINER" game
  [ "$STATE" != missing ] || fail "container not found; run Install first"
  game_id=$CID
  if [ "$STATE" = running ]; then
    echo "already running"
    exit 0
  fi
  # Unity opens its log as a regular file; /dev/stdout can fail here under
  # rootless Podman. Preserve the inode/SELinux label when resetting the log.
  private_file_if_present "$GAME_LOG"
  : > "$GAME_LOG" || fail "cannot prepare game log: $GAME_LOG"
  printf 'Game log: %s\n' "$GAME_LOG"
  if ! systemd-run --user --collect --scope --quiet -- podman start "$game_id"; then
    fail "Scope creation failed; cannot start Valheim outside Servo's lifetime"
  fi
  wait_ready
  ;;

stop)
  inspect_owned "$CONTAINER" game
  [ "$STATE" = running ] || { echo "already stopped"; exit 0; }
  game_id=$CID
  podman kill --signal SIGINT "$game_id" || fail "could not signal Valheim"
  deadline=$(( $(date +%s) + STOP_TIMEOUT ))
  while :; do
    inspect_owned "$CONTAINER" game
    [ "$CID" = "$game_id" ] || fail "container disappeared during shutdown"
    [ "$STATE" = running ] || { echo "Valheim stopped"; exit 0; }
    [ "$(date +%s)" -lt "$deadline" ] || fail "Valheim did not stop within ${STOP_TIMEOUT}s; refusing to force-kill"
    sleep 2
  done
  ;;

install|update)
  validate_config
  private_dir "$DATA"
  require_stopped
  for dir in server saves runtime; do make_private_dir "$DATA/$dir"; done
  for file in update-pending steamcmd.log image-digest valheim.log; do private_file_if_present "$DATA/runtime/$file"; done
  # SIGKILL on cancellation cannot run shell traps. This marker is removed
  # only after the complete operation, and the named updater prevents overlap.
  : > "$DATA/runtime/update-pending" || fail "cannot mark update pending"
  podman pull -q "$IMAGE" || fail "image pull failed"
  image_id=$(podman image inspect --format '{{.Id}}' "$IMAGE") || fail "cannot resolve pulled image"
  [ -n "$image_id" ] || fail "empty image ID"
  inspect_owned "$UPDATER" updater
  [ "$STATE" != running ] || fail "an updater is still running"
  if [ "$STATE" != missing ]; then podman rm "$CID" || fail "cannot remove stopped updater"; fi
  previous=$(installed_build) || exit 1
  # Each updater starts with an empty Steam cache. An in-place update needs
  # the installed build's depot manifest from that cache, and Steam denies
  # superseded manifests ("state is 0x6 after update job"). Without the app
  # manifest, SteamCMD validates the existing files against the current build
  # and downloads only what differs. Files a newer build drops stay behind.
  if [ -e "$MANIFEST" ]; then rm "$MANIFEST" || fail "cannot remove Steam app manifest"; fi
  # Keep the updater container after exit until success has been checked. A
  # cancelled caller cannot silently authorize Start, even if SteamCMD finishes.
  # Straight after app_info_update, app_update can start before the refreshed
  # metadata arrives and fail ("Missing configuration", "Timed out waiting for
  # update to start"). Printing the app info first waits for the metadata.
  echo "checking/downloading Valheim with SteamCMD ..."
  update_code=0
  podman run --name "$UPDATER" \
    --label "dev.servo.valheim.data=$DATA" --label dev.servo.valheim.role=updater \
    --userns=keep-id:uid=1000,gid=1000 --user 1000:1000 --umask 077 \
    --restart=no --timeout 3300 --stop-signal SIGTERM \
    -v "$DATA:/data:Z" --entrypoint /bin/bash \
    "$image_id" /home/steam/steamcmd/steamcmd.sh \
    +@ShutdownOnFailedCommand 1 +@NoPromptForPassword 1 \
    +force_install_dir /data/server +login anonymous +app_info_update 1 \
    +app_info_print 896660 +app_update 896660 validate +quit \
    > "$DATA/runtime/steamcmd.log" 2>&1 || update_code=$?
  # The log keeps the long app info dump for diagnosis; the output omits it.
  sed '/AppID : 896660,/,/^}/d' "$DATA/runtime/steamcmd.log"
  [ "$update_code" -eq 0 ] || fail "SteamCMD failed; rerun Install or Update"
  grep -F "Success! App '896660' fully installed." "$DATA/runtime/steamcmd.log" >/dev/null || fail "SteamCMD did not confirm success"
  [ ! -L "$DATA/server/valheim_server.x86_64" ] && [ -f "$DATA/server/valheim_server.x86_64" ] && [ -x "$DATA/server/valheim_server.x86_64" ] || fail "Valheim executable is missing"
  # SteamCMD has reported "already up to date" for an outdated install, so
  # its success line is not proof. Check the build Steam reported this run.
  current=$(public_build) || exit 1
  installed=$(installed_build) || exit 1
  case "$current" in ''|*[!0-9]*) fail "SteamCMD did not report the current public build" ;; esac
  [ "$installed" = "$current" ] || fail "SteamCMD reported success, but installed build ${installed:-unknown} is not the current public build $current; rerun Update"
  echo "Valheim build: ${previous:-none} -> $installed"
  require_stopped
  if [ "$STATE" != missing ]; then podman rm "$CID" || fail "cannot remove updater"; fi
  inspect_owned "$CONTAINER" game
  if [ "$STATE" != missing ]; then podman rm "$CID" || fail "cannot remove stopped game container"; fi
  # Include the log in the volume's SELinux relabel when creating the runtime.
  [ -e "$GAME_LOG" ] || { : > "$GAME_LOG" || fail "cannot create game log: $GAME_LOG"; }
  create_game || fail "cannot create game container"
  podman image inspect --format '{{json .RepoDigests}}' "$image_id" > "$DATA/runtime/image-digest" || fail "cannot record image digest"
  rm "$DATA/runtime/update-pending" || fail "cannot finish update"
  echo "installed/updated; game remains stopped. Image: $image_id"
  ;;

backup)
  require_stopped
  private_dir "$DATA"
  private_dir "$DATA/saves"
  BACKUPS=${SERVO_BACKUP_DIR:-}
  case "$BACKUPS" in /*) ;; *) fail "SERVO_BACKUP_DIR must be absolute" ;; esac
  private_dir "$BACKUPS"
  special=$(find "$DATA/saves" ! -type d ! -type f -print) || fail "cannot inspect saves"
  [ -z "$special" ] || fail "saves contain links or special files; refusing backup"
  temporary=$(mktemp -d "$BACKUPS/.backup-XXXXXXXX") || exit 1
  trap 'rm -rf "$temporary"' 0
  trap 'exit 1' HUP INT TERM
  archive="$BACKUPS/valheim-$(date +%Y%m%d-%H%M%S)-${temporary##*.backup-}.tar.gz"
  tar --hard-dereference -czf "$temporary/archive.tar.gz" -C "$DATA" saves || exit 1
  # Never publish an archive that our restore path would refuse.
  validate_archive "$temporary/archive.tar.gz" "$temporary"
  mv "$temporary/archive.tar.gz" "$archive" || exit 1
  printf '%s\n' "$archive"
  ;;

restore)
  require_stopped
  private_dir "$DATA"
  if [ -e "$DATA/saves" ] || [ -L "$DATA/saves" ]; then private_dir "$DATA/saves"; fi
  archive=${2:-}
  [ ! -L "$archive" ] && [ -f "$archive" ] || fail "archive must be a regular file"
  staging=$(mktemp -d "$DATA/.restore-XXXXXXXX") || exit 1
  trap 'rm -rf "$staging"' 0
  trap 'exit 1' HUP INT TERM
  validate_archive "$archive" "$staging"
  mkdir "$staging/extracted" || exit 1
  tar --no-same-owner --no-same-permissions -xzf "$archive" -C "$staging/extracted" || exit 1
  private_dir "$staging/extracted/saves"
  rm -rf "$DATA/saves" || exit 1
  mv "$staging/extracted/saves" "$DATA/saves" || exit 1
  echo "restored saves"
  ;;

uninstall)
  require_stopped
  if [ "$STATE" != missing ]; then podman rm "$CID" || fail "cannot remove updater"; fi
  inspect_owned "$CONTAINER" game
  if [ "$STATE" != missing ]; then podman rm "$CID" || fail "cannot remove game container"; fi
  echo "owned containers removed; cached images and backups kept"
  ;;
esac
