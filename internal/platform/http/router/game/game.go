// Package game is the dashboard's game-operation boundary.
package game

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Data-Corruption/Servo/internal/app"
	"github.com/Data-Corruption/Servo/internal/ops"
	"github.com/Data-Corruption/Servo/internal/platform/database/config"
	"github.com/Data-Corruption/Servo/internal/platform/http/middleware"
	"github.com/Data-Corruption/Servo/internal/types"
	"github.com/Data-Corruption/Servo/pkg/xhttp"
	"github.com/go-chi/chi/v5"
)

func Register(a *app.App, r chi.Router) {
	r.Get("/", dashboard(a))
	r.Get("/api/status", status(a))
	r.Post("/api/op/{op}", operation(a))
	r.Get("/api/backups", backups(a))
	r.Get("/api/backups/{name}", download(a))
	r.Get("/bg/dashboard", Background(a, "dashboard"))
	r.Post("/settings/driver/activate", activate(a))
}
func fail(w http.ResponseWriter, r *http.Request, code int, message string, err error) {
	xhttp.Error(r.Context(), w, &xhttp.Err{Code: code, Msg: message, Err: err})
}
func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func dashboard(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := config.View(a.DB)
		if err != nil {
			fail(w, r, 500, "Cannot read configuration", err)
			return
		}
		data := a.UI.PageData("Servo", a.BuildInfo().Version)
		perms := middleware.SessionPerms(r)
		data["CanControl"] = perms.Has(types.PermGameControl)
		data["CanBackup"] = perms.Has(types.PermGameBackup)
		data["CanRestore"] = perms.Has(types.PermGameRestore)
		data["CanListBackups"] = perms.Has(types.PermGameBackup) || perms.Has(types.PermGameRestore)
		data["IsAdmin"] = perms.Has(types.PermAdmin)
		data["HasDriver"] = cfg.ActiveDriver != ""
		data["DriversDir"] = a.Layout.Drivers
		data["HasBackground"] = cfg.DashboardBackground != ""
		data["ForcedTheme"] = cfg.ForcedTheme
		data["BackgroundBlur"] = cfg.BackgroundBlur
		data["ContentAlign"] = cfg.ContentAlign
		data["GameAddress"] = cfg.GameAddress
		data["GamePassword"] = cfg.GamePassword
		if err := a.UI.Execute(w, "dashboard.html", data); err != nil {
			xhttp.Error(r.Context(), w, err)
		}
	}
}

type StatusResponse struct {
	Op         ops.Snapshot  `json:"op"`
	Game       ops.GameState `json:"game"`
	Log        string        `json:"log,omitempty"`
	Offset     int64         `json:"offset"`
	NextWindow time.Time     `json:"nextWindow"`
	Timezone   string        `json:"timezone"`
}

func status(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Ops == nil {
			fail(w, r, 503, "Game runtime unavailable", nil)
			return
		}
		response := StatusResponse{Op: a.Ops.Status(), Game: a.Poller.Game(r.Context()), Timezone: time.Now().Location().String()}
		cfg, err := config.View(a.DB)
		if err != nil {
			fail(w, r, 500, "Cannot read configuration", err)
			return
		}
		response.NextWindow = ops.NextWindow(cfg, time.Now())
		if middleware.SessionPerms(r).Has(types.PermAdmin) {
			offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
			snapshot, output, next := a.Ops.Poll(r.URL.Query().Get("id"), offset)
			response.Op = snapshot
			response.Log = string(output)
			response.Offset = next
		} else {
			if response.Op.Last != nil {
				response.Op.Last.Tail = ""
				if !response.Op.Last.Success {
					response.Op.Last.Detail = "Operation did not complete; ask an administrator to inspect the game and operation log"
				}
			}
			if response.Game.Error != "" {
				response.Game.Error = "Status probe failed; ask an administrator to inspect the driver"
			}
		}
		jsonResponse(w, response)
	}
}
func permission(op ops.Op) types.Perm {
	switch op {
	case ops.OpInstall, ops.OpUninstall:
		return types.PermAdmin
	case ops.OpBackup:
		return types.PermGameBackup
	case ops.OpRestore:
		return types.PermGameRestore
	default:
		return types.PermGameControl
	}
}
func operation(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := ops.Op(chi.URLParam(r, "op"))
		if !ops.ValidOps[op] {
			fail(w, r, 400, "Unknown operation", nil)
			return
		}
		if err := middleware.RequirePerm(r, permission(op)); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		if a.Ops == nil {
			fail(w, r, 503, "Game runtime unavailable", nil)
			return
		}
		var args []string
		if op == ops.OpRestore {
			var body struct {
				Archive string `json:"archive"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Archive == "" {
				fail(w, r, 400, "Archive name required", err)
				return
			}
			args = []string{body.Archive}
		}
		if _, err := a.Ops.Start(op, args...); err != nil {
			if errors.Is(err, ops.ErrBusy) || errors.Is(err, ops.ErrNoDriver) {
				fail(w, r, 409, err.Error(), nil)
			} else {
				fail(w, r, 409, "Cannot start operation; check the active driver and archive", err)
			}
			return
		}
		a.Poller.Invalidate()
		w.WriteHeader(http.StatusAccepted)
	}
}
func activate(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := middleware.RequirePerm(r, types.PermAdmin); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			fail(w, r, 400, "Driver name required", err)
			return
		}
		if a.Ops == nil {
			fail(w, r, 503, "Game runtime unavailable", nil)
			return
		}
		info, err := a.Ops.Activate(r.Context(), body.Name)
		if err != nil {
			fail(w, r, 409, err.Error(), err)
			return
		}
		a.Poller.Invalidate()
		a.Sched.Poke()
		jsonResponse(w, info)
	}
}
func backups(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		perms := middleware.SessionPerms(r)
		if !perms.Has(types.PermGameBackup) && !perms.Has(types.PermGameRestore) {
			fail(w, r, 403, "Insufficient permissions", nil)
			return
		}
		if a.Ops == nil {
			fail(w, r, 503, "Game runtime unavailable", nil)
			return
		}
		env, err := a.Ops.Env()
		if errors.Is(err, ops.ErrNoDriver) {
			jsonResponse(w, []ops.BackupInfo{})
			return
		}
		if err != nil {
			fail(w, r, 409, "Cannot read active driver", err)
			return
		}
		files, err := ops.ListBackups(env.BackupDir)
		if err != nil {
			fail(w, r, 500, "Cannot list backups", err)
			return
		}
		if files == nil {
			files = []ops.BackupInfo{}
		}
		jsonResponse(w, files)
	}
}
func download(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := middleware.RequirePerm(r, types.PermGameBackup); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		if a.Ops == nil {
			fail(w, r, 503, "Game runtime unavailable", nil)
			return
		}
		env, err := a.Ops.Env()
		if err != nil {
			fail(w, r, 404, "Backup unavailable", err)
			return
		}
		name := chi.URLParam(r, "name")
		if _, err := ops.ResolveBackup(env.BackupDir, name); err != nil {
			fail(w, r, 404, "Backup unavailable", err)
			return
		}
		root, err := os.OpenRoot(env.BackupDir)
		if err != nil {
			fail(w, r, 404, "Backup unavailable", err)
			return
		}
		defer root.Close()
		file, err := root.Open(name)
		if err != nil {
			fail(w, r, 404, "Backup unavailable", err)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			fail(w, r, 404, "Backup unavailable", err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	}
}
func Background(a *app.App, target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := config.View(a.DB)
		if err != nil {
			fail(w, r, 500, "Background unavailable", err)
			return
		}
		name := cfg.LoginBackground
		if target == "dashboard" {
			name = cfg.DashboardBackground
		}
		if name == "" {
			http.NotFound(w, r)
			return
		}
		root, err := os.OpenRoot(a.Layout.Backgrounds)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		file, err := root.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, name, info.ModTime(), file)
	}
}
