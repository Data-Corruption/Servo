package types

import (
	"fmt"
	"strings"
	"time"

	"github.com/Data-Corruption/Servo/internal/build"
)

type Configuration struct {
	// game server / driver
	// ActiveDriver is the filename of the active driver in the drivers dir.
	// Empty = none activated.
	ActiveDriver string `json:"activeDriver"`
	// RestartTime is the daily restart window as "HH:MM" (host-local time).
	RestartTime    string `json:"restartTime"`
	RestartEnabled bool   `json:"restartEnabled"`
	// BackupsEnabled makes the restart window take a backup (and enables
	// backup-only runs while the server is offline).
	BackupsEnabled bool `json:"backupsEnabled"`
	// BackupRetention is how many archives to keep, newest first.
	BackupRetention int `json:"backupRetention"`
	// NotifyLeadMinutes is how many minutes before the restart window players
	// are warned via the notify verb. 0 = no warning.
	NotifyLeadMinutes int `json:"notifyLeadMinutes"`
	// Uploaded background image filenames (within the backgrounds dir).
	// Empty = default background.
	LoginBackground     string `json:"loginBackground"`
	DashboardBackground string `json:"dashboardBackground"`
	// appearance
	// ForcedTheme pins a DaisyUI theme for everyone and hides the dark mode
	// toggle. Empty = per-user light/dark toggle.
	ForcedTheme string `json:"forcedTheme"`
	// BackgroundBlur is the background image blur radius in px (0 = sharp).
	BackgroundBlur int `json:"backgroundBlur"`
	// ContentAlign floats the content column left/center/right on wide
	// displays. Empty = center.
	ContentAlign string `json:"contentAlign"`
	// game server connection info surfaced on the dashboard (copy buttons)
	GameAddress  string `json:"gameAddress"`
	GamePassword string `json:"gamePassword"`

	LogLevel string `json:"logLevel"`
	// UIBind is the self-signed HTTPS dashboard listener
	// (defaults to ":8829" for LAN access in production).
	UIBind string `json:"uiBind"`
	// ProxyBind is the optional loopback-only plain HTTP listener for local
	// reverse proxies such as Caddy (defaults to "127.0.0.1:8830"). Empty = disabled.
	ProxyBind string `json:"proxyBind"`

	UpdateNotifications    bool      `json:"updateNotifications"`
	LastUpdateCheck        time.Time `json:"lastUpdateCheck"`
	BackgroundUpdateChecks bool      `json:"backgroundUpdateChecks"`
	UpdateCheckSource      string    `json:"updateCheckSource"`
	LatestUpdateVersion    string    `json:"latestUpdateVersion"`

	// LastShutdownVersion is compared with the running version after a restart
	// to infer whether an update occurred.
	LastShutdownVersion string `json:"lastShutdownVersion"`

	// Dashboard auth (sessions live in the sessions table, not in config).
	Credentials []Credential `json:"credentials"`
	// Incremented whenever all retained service components report ready.
	StartCounter int `json:"startCounter"`
}

// Credential is a UI login credential. Passwords are stored Argon2id-hashed.
type Credential struct {
	Username string `json:"username"`
	PassHash string `json:"passHash"`
	PassSalt string `json:"passSalt"`
	Perms    Perm   `json:"perms"`
}

// NormalizeUsername returns the canonical credential identity used for login,
// listing, removal, session attribution, and uniqueness checks.
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func DefaultConfig(buildInfo build.BuildInfo) Configuration {
	// Production is ready for LAN access and a local reverse proxy.
	uiBind := fmt.Sprintf(":%d", buildInfo.ServiceDefaultPort)
	proxyBind := fmt.Sprintf("127.0.0.1:%d", buildInfo.ServiceDefaultPort+1)
	// Development builds bypass authentication; keep their default listener local.
	if buildInfo.DevMode {
		uiBind = "127.0.0.1" + uiBind
		proxyBind = ""
	}

	return Configuration{
		LogLevel:               buildInfo.DefaultLogLevel,
		RestartTime:            "04:00",
		BackupRetention:        5,
		NotifyLeadMinutes:      10,
		UIBind:                 uiBind,
		ProxyBind:              proxyBind,
		UpdateNotifications:    true,
		BackgroundUpdateChecks: true,
		LastUpdateCheck:        time.Time{},
	}
}
