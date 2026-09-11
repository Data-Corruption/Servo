#!/bin/sh
# Servo driver: Palworld via podman on Fedora (Driver API v1)
#
# Drives https://github.com/thijsvanloef/palworld-server-docker rootless.
# Install: scp fedora-palworld.sh host:~/.servo/drivers/ && chmod +x it,
# then activate in the Servo settings page and press Install once.
#
# Edit the config section below (passwords!) before installing.
#
# Rootless podman notes:
#   - The container does NOT auto-start on host reboot. Start it from the
#     Servo dashboard, or wire up Quadlet yourself if you care.
#   - `start` launches the container in its own transient systemd scope
#     (systemd-run) so it lives outside servo.service's cgroup — a Servo
#     stop/restart/self-update can't take the game down with it.
#   - --userns=keep-id (podman >= 4.3) maps the host user onto the uid the
#     server runs as, keeping volume files host-owned (see create_container).
#   - Ports >1024 only (8211/27015 are fine).
#   - -v ...:Z relabels the data dir for SELinux (Fedora default: enforcing).

set -u
umask 077

# --- config (edit me) ----------------------------------------------------

IMAGE="docker.io/thijsvanloef/palworld-server-docker:latest"
CONTAINER="palworld-server"
DATA="$SERVO_DATA_DIR"  # exclusive to this driver; Servo creates it

GAME_PORT=8211
QUERY_PORT=27015
PLAYERS=16
SERVER_NAME="Servo Palworld"
SERVER_PASSWORD="changeme"       # empty = no password
ADMIN_PASSWORD="changeme-admin"  # used by the in-container REST client
STOP_TIMEOUT=90                  # seconds; Palworld saves on shutdown
START_READY_TIMEOUT=300          # seconds to wait for the REST API after start

# List the server in the in-game community server browser? Case sensitive,
# must be exactly "true" or "false" (consumed by a shell script in the image).
# Toggling this requires a reinstall (Install button) to recreate the container.
#
# Port exposure per case (router forward + firewalld must match):
#   COMMUNITY=false (private, friends direct-connect via ip/ddns:GAME_PORT):
#     - forward/open GAME_PORT/udp only
#     - QUERY_PORT is not even published on the host (see create_container)
#   COMMUNITY=true (publicly listed):
#     - forward/open GAME_PORT/udp AND QUERY_PORT/udp
#     - set a real SERVER_PASSWORD — the listing makes you discoverable
#   The REST API (8212/tcp) is never published; rest-cli runs inside the
#   container, so nothing to forward and nothing to firewall.
#
#   firewalld: sudo firewall-cmd --permanent --add-port=8211/udp
#              (+ --add-port=27015/udp only if COMMUNITY=true)
#              sudo firewall-cmd --reload
COMMUNITY=false

# Game settings. The image regenerates PalWorldSettings.ini from these on
# every start, so this block is the source of truth — don't edit the ini by
# hand (it gets overwritten). After changing values here, press Install in
# Servo to recreate the container; the data dir (world save) is preserved.
DEATH_PENALTY="Item"             # None | Item | ItemAndEquipment | All (game default: All)
EXP_RATE=3.5                     # XP multiplier (default 1); fixes the high-level grind
PAL_CAPTURE_RATE=1.5             # capture chance multiplier (default 1)
DAYTIME_SPEEDRATE=1              # >1 = shorter days (default 1)
NIGHTTIME_SPEEDRATE=1.5          # >1 = shorter nights (default 1)
PAL_EGG_DEFAULT_HATCHING_TIME=2  # hours for massive eggs (default 72!); values <=1 clamp to 1
SUPPLY_DROP_SPAN=180             # meteorite / supply drop interval in minutes (default 180)
# Some Palworld releases write this worker limit to the generated INI but
# ignore it at runtime unless a matching WorldOption.sav is generated.
BASE_CAMP_WORKER_MAX_NUM=15      # workers assigned to one base (game default 15)
BASE_CAMP_MAX_NUM_IN_GUILD=4     # bases allowed per guild (game default 4)

# When you verify this driver against a specific image release, pin it here
# to power Servo's (soft) staleness badge. Empty = badge disabled.
TARGET_CONTAINER_VERSION=""

# --- helpers ------------------------------------------------------------

need() {
  _missing=0
  for _tool in "$@"; do
    if ! command -v "$_tool" >/dev/null 2>&1; then
      echo "$_tool"
      _missing=1
    fi
  done
  return $_missing
}

container_exists() { podman container exists "$CONTAINER"; }

container_running() {
  _exists=0
  podman container exists "$CONTAINER" || _exists=$?
  case $_exists in 0) ;; 1) return 3 ;; *) return 1 ;; esac
  _running=$(podman inspect --format '{{.State.Running}}' "$CONTAINER") || return 1
  case $_running in true) return 0 ;; false) return 3 ;; *) return 1 ;; esac
}

# The image bundles rest-cli and jq. Keeping JSON parsing in the container
# avoids adding host dependencies and leaves the REST port unpublished.
rest() { podman exec "$CONTAINER" rest-cli --no-flush-log "$@"; }

rest_jq() {
  _api=$1
  _filter=$2
  podman exec "$CONTAINER" bash -o pipefail -c \
    'rest-cli --no-flush-log "$1" | jq -r "$2"' servo-rest "$_api" "$_filter"
}

wait_ready() {
  _deadline=$(( $(date +%s) + START_READY_TIMEOUT ))
  echo "waiting for Palworld REST API (up to ${START_READY_TIMEOUT}s) ..."
  while :; do
    _status=0
    container_running || _status=$?
    case $_status in
      0) ;;
      3) echo "container stopped before Palworld REST API became ready" >&2; return 1 ;;
      *) echo "status probe failed while waiting for Palworld REST API" >&2; return 1 ;;
    esac
    if _version=$(rest_jq info '.version // empty' 2>/dev/null) && [ -n "$_version" ]; then
      echo "Palworld REST API ready"
      return 0
    fi
    if [ "$(date +%s)" -ge "$_deadline" ]; then
      echo "Palworld REST API did not become ready within ${START_READY_TIMEOUT}s" >&2
      return 1
    fi
    sleep 5
  done
}

create_container() {
  mkdir -p "$DATA"
  # publish the query port only when the server is publicly listed;
  # an unpublished port can't be accidentally exposed later
  set -- -p "$GAME_PORT:8211/udp"
  if [ "$COMMUNITY" = "true" ]; then
    set -- "$@" -p "$QUERY_PORT:27015/udp"
  fi
  # keep-id maps the host user onto container uid/gid 1000 (the PUID the
  # image runs the server as), so files written into the volume are owned by
  # the host user — without it they land subuid-owned, breaking backup
  # (unreadable 0700 dirs like Pal/.sentry-native), restore, and uninstall.
  podman create --name "$CONTAINER" "$@" \
    --userns=keep-id:uid=1000,gid=1000 \
    -v "$DATA:/palworld:Z" \
    -e PUID=1000 -e PGID=1000 \
    -e PORT=8211 \
    -e PLAYERS="$PLAYERS" \
    -e SERVER_NAME="$SERVER_NAME" \
    -e SERVER_PASSWORD="$SERVER_PASSWORD" \
    -e ADMIN_PASSWORD="$ADMIN_PASSWORD" \
    -e REST_API_ENABLED=true \
    -e REST_API_PORT=8212 \
    -e RCON_ENABLED=false \
    -e COMMUNITY="$COMMUNITY" \
    -e DEATH_PENALTY="$DEATH_PENALTY" \
    -e EXP_RATE="$EXP_RATE" \
    -e PAL_CAPTURE_RATE="$PAL_CAPTURE_RATE" \
    -e DAYTIME_SPEEDRATE="$DAYTIME_SPEEDRATE" \
    -e NIGHTTIME_SPEEDRATE="$NIGHTTIME_SPEEDRATE" \
    -e PAL_EGG_DEFAULT_HATCHING_TIME="$PAL_EGG_DEFAULT_HATCHING_TIME" \
    -e SUPPLY_DROP_SPAN="$SUPPLY_DROP_SPAN" \
    -e BASE_CAMP_WORKER_MAX_NUM="$BASE_CAMP_WORKER_MAX_NUM" \
    -e BASE_CAMP_MAX_NUM_IN_GUILD="$BASE_CAMP_MAX_NUM_IN_GUILD" \
    "$IMAGE"
}

# --- verbs ----------------------------------------------------------------

case "${1:-}" in

describe)
  echo "DRIVER_API=1"
  echo "NAME=Palworld (Podman, Fedora)"
  echo "GAME=palworld"
  echo "CAPABILITIES=restore,uninstall,notify,players,metrics,version,container-version"
  echo "CONTAINERIZED=true"
  [ -n "$TARGET_CONTAINER_VERSION" ] && echo "TARGET_CONTAINER_VERSION=$TARGET_CONTAINER_VERSION"
  exit 0
  ;;

deps)
  need podman tar gzip systemd-run awk date mkdir mktemp mv rm
  ;;

status)
  container_running
  exit $?
  ;;

start)
  _status=0
  container_running || _status=$?
  case $_status in 0|3) ;; *) echo "status probe failed" >&2; exit 1 ;; esac
  if [ "$_status" -eq 0 ]; then
    echo "already running"
  else
    if ! container_exists; then
      echo "container not found — run install first" >&2
      exit 1
    fi
    # Start in a transient scope so conmon + the game land OUTSIDE Servo's
    # service cgroup (systemd's default KillMode=control-group would otherwise
    # kill them on any Servo stop/restart/self-update). See docs/content/drivers.md.
    if ! systemd-run --user --collect --scope --quiet -- podman start "$CONTAINER"; then
      echo "Scope creation failed; refusing to couple the game to Servo's lifetime." >&2
      exit 1
    fi
  fi
  wait_ready
  ;;

stop)
  _status=0
  container_running || _status=$?
  case $_status in 0) ;; 3) echo "already stopped"; exit 0 ;; *) echo "status probe failed" >&2; exit 1 ;; esac
  podman stop -t "$STOP_TIMEOUT" "$CONTAINER"
  ;;

install)
  echo "pulling $IMAGE ..."
  podman pull -q "$IMAGE" || exit 1
  if container_exists; then
    echo "container already exists, recreating"
    podman rm -f "$CONTAINER" || exit 1
  fi
  create_container || exit 1
  echo "installed. data dir: $DATA"
  ;;

uninstall)
  # Server is stopped. Remove what install created outside $SERVO_DATA_DIR;
  # Servo deletes the data dir itself afterwards (backups are kept).
  container_exists && { podman rm -f "$CONTAINER" || exit 1; }
  podman rmi "$IMAGE" 2>/dev/null || true  # may be shared/absent; best effort
  echo "container and image removed"
  ;;

update)
  # convention: check first, no-op success if already current
  echo "pulling $IMAGE ..."
  podman pull -q "$IMAGE" || exit 1
  new_id=$(podman image inspect --format '{{.Id}}' "$IMAGE") || exit 1
  cur_id=$(podman inspect --format '{{.Image}}' "$CONTAINER" 2>/dev/null || echo none)
  if [ "$new_id" = "$cur_id" ]; then
    echo "already up to date"
    exit 0
  fi
  echo "new image found, recreating container (data dir is preserved)"
  container_exists && { podman rm -f "$CONTAINER" || exit 1; }
  create_container || exit 1
  echo "updated"
  ;;

backup)
  # Only Pal/Saved matters: world saves, player data, and ini config. The
  # rest of $DATA is the steamcmd-installed server (~4.5GB), fully
  # re-downloadable via Install/Update — archiving it would waste space.
  [ -d "$DATA/Pal/Saved" ] || { echo "no save data to back up (has the server ever started?)" >&2; exit 1; }
  temporary=$(mktemp -d "$SERVO_BACKUP_DIR/.backup-XXXXXXXX") || exit 1
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM
  f="$SERVO_BACKUP_DIR/palworld-$(date +%Y%m%d-%H%M%S)-${temporary##*.backup-}.tar.gz"
  echo "archiving $DATA/Pal/Saved ..."
  tar -czf "$temporary/archive.tar.gz" -C "$DATA" Pal/Saved || exit 1
  mv "$temporary/archive.tar.gz" "$f" || exit 1
  echo "$f"
  ;;

restore)
  archive="${2:?archive path required}"
  [ -f "$archive" ] || { echo "archive not found: $archive" >&2; exit 1; }
  # Validate and extract before removing the existing saves. No symlinks,
  # hard links, devices, or paths outside Pal/Saved are accepted.
  staging=$(mktemp -d "$DATA/.restore-XXXXXXXX") || exit 1
  trap 'rm -rf "$staging"' EXIT HUP INT TERM
  tar -tzf "$archive" > "$staging/members" || exit 1
  awk 'BEGIN { bad=0; count=0 } { count++; if ($0 !~ /^Pal\/Saved(\/|$)/ || $0 ~ /(^|\/)\.\.(\/|$)/) bad=1 } END { exit (bad || !count) }' "$staging/members" || exit 1
  tar -tvzf "$archive" > "$staging/types" || exit 1
  awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" { bad=1 } END { exit bad }' "$staging/types" || exit 1
  tar --no-same-owner -xzf "$archive" -C "$staging" || exit 1
  [ -d "$staging/Pal/Saved" ] || exit 1
  echo "replacing Pal/Saved from validated archive ..."
  rm -rf "$DATA/Pal/Saved" || exit 1
  mkdir -p "$DATA/Pal" || exit 1
  mv "$staging/Pal/Saved" "$DATA/Pal/Saved" || exit 1
  echo "restored"
  ;;

notify)
  msg="${2:?message required}"
  container_running || { echo "server offline, skipping notify" >&2; exit 1; }
  json=$(printf '%s' "$msg" | podman exec -i "$CONTAINER" jq -Rs '{message: .}') || exit 1
  rest announce "$json"
  ;;

players)
  container_running || exit 1
  rest_jq players '.players[]?.name // empty'
  ;;

metrics)
  container_running || exit 1
  summary=$(rest_jq metrics '.serverfps | select(type == "number") | "\(.) FPS"') || exit 1
  [ -n "$summary" ] || { echo "metrics response missing serverfps" >&2; exit 1; }
  echo "$summary"
  ;;

container-version)
  ver=$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$IMAGE" 2>/dev/null)
  [ -n "$ver" ] || exit 1
  echo "$ver"
  ;;

version)
  container_running || exit 1
  ver=$(rest_jq info '.version // empty') || exit 1
  [ -n "$ver" ] || { echo "info response missing version" >&2; exit 1; }
  echo "$ver"
  ;;

*)
  echo "unknown verb: ${1:-<none>}" >&2
  exit 4
  ;;
esac
