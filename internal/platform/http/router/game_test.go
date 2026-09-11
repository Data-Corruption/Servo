//go:build !windows

package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/Servo/internal/app"
	"github.com/Data-Corruption/Servo/internal/layout"
	"github.com/Data-Corruption/Servo/internal/ops"
	"github.com/Data-Corruption/Servo/internal/platform/database/config"
	"github.com/Data-Corruption/Servo/internal/platform/database/sessions"
	"github.com/Data-Corruption/Servo/internal/platform/http/middleware"
	"github.com/Data-Corruption/Servo/internal/types"
	"github.com/Data-Corruption/Servo/pkg/crypto"
)

func gameApp(t *testing.T) *app.App {
	t.Helper()
	a := newRouterTestApp(t)
	a.Layout = layout.FromStorage(filepath.Join(t.TempDir(), "servo"), "servo")
	if err := a.Layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$1" in
 describe) printf 'DRIVER_API=1\nNAME=Test game\nCAPABILITIES=restore,uninstall\n' ;;
 deps) exit 0 ;;
 status) exit 3 ;;
 install) echo 'private-driver-diagnostic'; sleep 1 ;;
 start) echo 'private-driver-diagnostic'; exit 1 ;;
 *) exit 0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(a.Layout.Drivers, "fixture.sh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Update(a.DB, func(cfg *types.Configuration) error { cfg.ActiveDriver = "fixture.sh"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.InitGame(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Ops.Close() })
	if _, _, err := a.Layout.EnsureDriver("fixture.sh"); err != nil {
		t.Fatal(err)
	}
	env, err := a.Ops.Env()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.BackupDir, "save.gz"), []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	return a
}
func tokenFor(t *testing.T, a *app.App, perms types.Perm) string {
	t.Helper()
	token, err := crypto.GenRandomString(16)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.Create(a.DB, crypto.Hash(token), sessions.Session{Username: "fixture", Perms: perms, Expiry: time.Now().Add(middleware.SessionDuration)}); err != nil {
		t.Fatal(err)
	}
	return token
}
func request(t *testing.T, handler http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "sprout_session", Value: token})
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
func TestGamePermissionBoundaries(t *testing.T) {
	a := gameApp(t)
	router := New(a)
	for _, tc := range []struct {
		name               string
		perms              types.Perm
		path, method, body string
		allowed            bool
	}{
		{"viewer status", 0, "/api/status", "GET", "", true},
		{"viewer start", 0, "/api/op/start", "POST", "{}", false},
		{"controller install", types.PermGameControl, "/api/op/install", "POST", "{}", false},
		{"controller uninstall", types.PermGameControl, "/api/op/uninstall", "POST", "{}", false},
		{"restore lists", types.PermGameRestore, "/api/backups", "GET", "", true},
		{"restore cannot download", types.PermGameRestore, "/api/backups/save.gz", "GET", "", false},
		{"backup downloads", types.PermGameBackup, "/api/backups/save.gz", "GET", "", true},
		{"backup cannot restore", types.PermGameBackup, "/api/op/restore", "POST", `{"archive":"save.gz"}`, false},
		{"settings cannot activate", types.PermServoSettings, "/settings/driver/activate", "POST", `{"name":"fixture.sh"}`, false},
		{"settings cannot clear images", types.PermServoSettings, "/settings/background/clear", "POST", `{"target":"login"}`, false},
		{"settings cannot blur", types.PermServoSettings, "/settings", "POST", `{"backgroundBlur":4}`, false},
		{"settings allowed", types.PermServoSettings, "/settings", "POST", `{"backupRetention":0}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := request(t, router, tokenFor(t, a, tc.perms), tc.method, tc.path, tc.body)
			if tc.allowed && res.Code >= 400 || !tc.allowed && res.Code != 403 {
				t.Fatalf("status %d: %s", res.Code, res.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		perms        types.Perm
		want, absent string
	}{{types.PermGameBackup, `data-op="backup"`, `data-op="start"`}, {types.PermGameRestore, `id="backups-list"`, `data-op="backup"`}} {
		res := request(t, router, tokenFor(t, a, tc.perms), "GET", "/", "")
		if !strings.Contains(res.Body.String(), tc.want) || strings.Contains(res.Body.String(), tc.absent) {
			t.Fatalf("permission UI mismatch: %s", res.Body.String())
		}
	}
}
func TestStatusRedactsPersistedDriverDiagnostics(t *testing.T) {
	a := gameApp(t)
	done, err := a.Ops.Start(ops.OpStart)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	router := New(a)
	for _, perms := range []types.Perm{0, types.PermGameControl, types.PermAdmin} {
		response := request(t, router, tokenFor(t, a, perms), "GET", "/api/status", "")
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		contains := strings.Contains(response.Body.String(), "private-driver-diagnostic")
		if contains != perms.Has(types.PermAdmin) {
			t.Fatalf("diagnostic boundary: perms=%v response=%s", perms, response.Body.String())
		}
	}
}
func TestSessionExpiryDoesNotRenewOrCancelOperation(t *testing.T) {
	a := gameApp(t)
	router := New(a)
	token := tokenFor(t, a, types.PermAdmin)
	before, err := sessions.Get(a.DB, crypto.Hash(token))
	if err != nil {
		t.Fatal(err)
	}
	res := request(t, router, token, "POST", "/api/op/install", "{}")
	if res.Code != 202 {
		t.Fatalf("install: %d %s", res.Code, res.Body.String())
	}
	for i := 0; i < 4; i++ {
		res = request(t, router, token, "GET", "/api/status", "")
		if res.Code != 200 || res.Header().Get("Set-Cookie") != "" {
			t.Fatal("poll renewed or lost session")
		}
	}
	after, err := sessions.Get(a.DB, crypto.Hash(token))
	if err != nil || !after.Expiry.Equal(before.Expiry) {
		t.Fatal("poll changed fixed expiry")
	}
	if _, err := a.DB.Exec(`UPDATE sessions SET expiry=? WHERE token_hash=?`, time.Now().Add(-time.Second).Unix(), crypto.Hash(token)); err != nil {
		t.Fatal(err)
	}
	if res := request(t, router, token, "GET", "/api/status", ""); res.Code != 401 {
		t.Fatalf("expired API: %d", res.Code)
	}
	if res := request(t, router, token, "GET", "/", ""); res.Code != 303 {
		t.Fatalf("expired page: %d", res.Code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for a.Ops.Busy() {
		if time.Now().After(deadline) {
			t.Fatal("operation did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if a.Ops.Status().Last.Outcome != "succeeded" {
		t.Fatalf("expiry cancelled operation: %+v", a.Ops.Status())
	}
	// A new router models auth-service recreation on daemon restart; the
	// unchanged database session and durable result are still observable.
	fresh := tokenFor(t, a, types.PermAdmin)
	res = request(t, New(a), fresh, "GET", "/api/status", "")
	if !strings.Contains(res.Body.String(), `"outcome":"succeeded"`) {
		t.Fatal(res.Body.String())
	}
}
func TestBackgroundUploadOriginPermissionsAndStorage(t *testing.T) {
	a := gameApp(t)
	router := New(a)
	admin := tokenFor(t, a, types.PermAdmin)
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aE1sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	upload := func(token, origin, path string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("target", "login")
		part, _ := writer.CreateFormFile("image", "../../private.png")
		_, _ = part.Write(png)
		_ = writer.Close()
		req := httptest.NewRequest("POST", "https://example.com"+path, &body)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "sprout_session", Value: token})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	for _, tc := range []struct {
		token, origin, path string
		want                int
	}{{admin, "https://elsewhere.test", "/settings/background", 403}, {tokenFor(t, a, types.PermServoSettings), "https://example.com", "/settings/background", 403}, {admin, "https://example.com", "/settings", 415}, {admin, "https://example.com", "/settings/background", 200}} {
		response := upload(tc.token, tc.origin, tc.path)
		if response.Code != tc.want {
			t.Fatalf("upload: %d %s", response.Code, response.Body.String())
		}
	}
	cfg, err := config.View(a.DB)
	if err != nil || cfg.LoginBackground == "" || strings.Contains(cfg.LoginBackground, "private") {
		t.Fatalf("upload filename: %+v %v", cfg, err)
	}
	response := request(t, router, "", "GET", "/bg/login", "")
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), png) {
		t.Fatal("login background must be public and serve saved image")
	}
	response = request(t, router, admin, "POST", "/settings/background/clear", `{"target":"login"}`)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.Layout.Backgrounds, cfg.LoginBackground)); !os.IsNotExist(err) {
		t.Fatal("cleared image remains")
	}
	if response := request(t, router, admin, "POST", "/settings", `{"restartTime":"25:00"}`); response.Code != 400 {
		t.Fatal("invalid schedule accepted")
	}
}
func TestCurrentOperationRecordHasStableIdentity(t *testing.T) {
	a := gameApp(t)
	router := New(a)
	token := tokenFor(t, a, types.PermAdmin)
	done, err := a.Ops.Start(ops.OpStart)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	response := request(t, router, token, "GET", "/api/status?offset=999999&id=obsolete", "")
	var value map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value["op"].(map[string]any)["id"] == "" || !strings.Contains(fmt.Sprint(value["log"]), "private-driver-diagnostic") {
		t.Fatal("old cursor failed to recover", value)
	}
}
