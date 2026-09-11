package settings

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Data-Corruption/Servo/internal/app"
	"github.com/Data-Corruption/Servo/internal/platform/database/config"
	"github.com/Data-Corruption/Servo/internal/platform/http/middleware"
	"github.com/Data-Corruption/Servo/internal/types"
	"github.com/Data-Corruption/Servo/pkg/xhttp"
)

const maxBackgroundBytes = 8 << 20 // 8 MiB

// allowed background image types → canonical extension
var backgroundTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// sniffImageExt returns the canonical extension for a supported image type
// based on the file's leading bytes. AVIF needs a manual check because
// http.DetectContentType doesn't know ISO-BMFF brands.
func sniffImageExt(head []byte) (string, bool) {
	if ext, ok := backgroundTypes[http.DetectContentType(head)]; ok {
		return ext, true
	}
	// The major brand is usually "avif"/"avis" but some encoders use "mif1"
	// and only list avif among the compatible brands, so scan the whole ftyp
	// box header region.
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		brands := string(head[8:min(len(head), 32)])
		if strings.Contains(brands, "avif") || strings.Contains(brands, "avis") {
			return ".avif", true
		}
	}
	return "", false
}

// backgroundTarget validates the login/dashboard discriminator used by the
// upload, clear, and serve endpoints.
func backgroundTarget(s string) (string, bool) {
	if s == "login" || s == "dashboard" {
		return s, true
	}
	return "", false
}

func handleUploadBackground(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := middleware.RequirePerm(r, types.PermAdmin); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBackgroundBytes+(64<<10))
		if err := r.ParseMultipartForm(maxBackgroundBytes); err != nil {
			http.Error(w, "Upload too large or malformed", 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		target, ok := backgroundTarget(r.FormValue("target"))
		if !ok {
			http.Error(w, "Invalid background target", 400)
			return
		}
		file, _, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "Image required", 400)
			return
		}
		defer file.Close()
		head := make([]byte, 512)
		n, _ := io.ReadFull(file, head)
		ext, ok := sniffImageExt(head[:n])
		if !ok {
			http.Error(w, "Unsupported image format", 415)
			return
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		temp, err := os.CreateTemp(a.Layout.Backgrounds, "upload-*")
		if err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		defer os.Remove(temp.Name())
		written, copyErr := io.Copy(temp, io.LimitReader(file, maxBackgroundBytes+1))
		syncErr := temp.Sync()
		closeErr := temp.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || written > maxBackgroundBytes {
			http.Error(w, "Cannot store image (8 MiB maximum)", 400)
			return
		}
		name := filepath.Base(temp.Name()) + ext
		path := filepath.Join(a.Layout.Backgrounds, name)
		if err := os.Rename(temp.Name(), path); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		var previous string
		_, err = config.Update(a.DB, func(cfg *types.Configuration) error {
			if target == "login" {
				previous = cfg.LoginBackground
				cfg.LoginBackground = name
			} else {
				previous = cfg.DashboardBackground
				cfg.DashboardBackground = name
			}
			return nil
		})
		if err != nil {
			_ = os.Remove(path)
			xhttp.Error(r.Context(), w, err)
			return
		}
		if previous != "" {
			if err := os.Remove(filepath.Join(a.Layout.Backgrounds, previous)); err != nil && !os.IsNotExist(err) {
				a.Log.Warnf("remove previous image: %v", err)
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}
func handleClearBackground(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := middleware.RequirePerm(r, types.PermAdmin); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		var body struct {
			Target string `json:"target"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		target, ok := backgroundTarget(body.Target)
		if !ok {
			http.Error(w, "Invalid background target", 400)
			return
		}
		var previous string
		_, err := config.Update(a.DB, func(cfg *types.Configuration) error {
			if target == "login" {
				previous = cfg.LoginBackground
				cfg.LoginBackground = ""
			} else {
				previous = cfg.DashboardBackground
				cfg.DashboardBackground = ""
			}
			return nil
		})
		if err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		if previous != "" {
			if err := os.Remove(filepath.Join(a.Layout.Backgrounds, previous)); err != nil && !os.IsNotExist(err) {
				a.Log.Warnf("%s", fmt.Errorf("remove cleared image: %w", err))
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}
