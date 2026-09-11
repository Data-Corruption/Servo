//go:build !windows

package router

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Data-Corruption/Servo/internal/platform/database/config"
	"github.com/Data-Corruption/Servo/internal/types"
	"github.com/Data-Corruption/Servo/pkg/crypto"
)

// Opt-in isolated browser fixture. No installed service or real game is used.
// SERVO_BROWSER_DIR receives connection.json; create expire to revoke fixture
// sessions or stop to finish. The fixture times out after ten minutes.
func TestBrowserFixture(t *testing.T) {
	directory := os.Getenv("SERVO_BROWSER_DIR")
	if directory == "" {
		t.Skip("opt-in browser fixture")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	a := gameApp(t)
	hash, salt, err := crypto.HashPassword("servo-preview")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Update(a.DB, func(cfg *types.Configuration) error {
		cfg.Credentials = []types.Credential{{Username: "admin", PassHash: hash, PassSalt: salt, Perms: types.PermAdmin}}
		cfg.GameAddress = "play.example.test:8211"
		cfg.GamePassword = "friends"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pollDone := make(chan struct{})
	go func() { defer close(pollDone); a.Poller.Run(ctx) }()
	defer func() { cancel(); _ = a.Ops.Close(); <-pollDone }()
	server := httptest.NewTLSServer(New(a))
	defer server.Close()
	data, _ := json.Marshal(map[string]string{"url": server.URL, "username": "admin", "password": "servo-preview"})
	if err := os.WriteFile(filepath.Join(directory, "connection.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("browser fixture timed out")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(directory, "expire")); err == nil {
				if _, err := a.DB.Exec(`UPDATE sessions SET expiry=0`); err != nil {
					t.Fatal(err)
				}
				_ = os.Remove(filepath.Join(directory, "expire"))
			}
			if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
				return
			}
		}
	}
}
