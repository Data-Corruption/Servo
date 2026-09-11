package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Native executables own their game shutdown contract. Process-tree and
// file-signaling policies are deliberately deferred (see the Windows icebox).
func configureProcess(cmd *exec.Cmd) {}
func executable(info os.FileInfo) bool {
	return info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(info.Name()), ".exe")
}
