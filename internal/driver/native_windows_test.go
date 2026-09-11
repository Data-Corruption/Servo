package driver

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeExecutableContract(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "fixture.go")
	binary := filepath.Join(directory, "fixture.exe")
	code := `package main
import("fmt";"os";"time")
func main(){switch os.Args[1]{case "describe":fmt.Println("DRIVER_API=1\nNAME=Windows fixture\nCAPABILITIES=notify");case "status":os.Exit(3);case "notify":fmt.Print(os.Args[2]+"|"+os.Getenv("SERVO_VERSION"));case "start":time.Sleep(time.Minute);default:os.Exit(4)}}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-o", binary, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v %s", err, output)
	}
	env := Env{DriverPath: binary, DataDir: directory, BackupDir: directory, AppVersion: "vTEST"}
	info, err := Describe(context.Background(), env)
	if err != nil || !info.Supports(VerbNotify) {
		t.Fatalf("describe: %+v %v", info, err)
	}
	status, err := GetStatus(context.Background(), env)
	if err != nil || status != StatusOffline {
		t.Fatalf("status: %v %v", status, err)
	}
	var output bytes.Buffer
	if code, err := Run(context.Background(), env, &output, VerbNotify, "literal & argument"); err != nil || code != 0 || output.String() != "literal & argument|vTEST" {
		t.Fatalf("argv/env: %d %v %q", code, err, output.String())
	}
	files, err := List(directory)
	if err != nil || len(files) != 1 || files[0] != "fixture.exe" {
		t.Fatalf("discovery: %v %v", files, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, env, nil, VerbStart); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
}
