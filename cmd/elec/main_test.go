package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateInstallPathRejectsBroadAndSymlinkPaths(t *testing.T) {
	if err := validateInstallPath("/elec"); err == nil {
		t.Fatal("single-level install path should be rejected")
	}

	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPath(filepath.Join(link, "elec")); err == nil {
		t.Fatal("path containing symlink component should be rejected")
	}
	if err := validateInstallPath(filepath.Join(realDir, "elec")); err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
}

func TestSecureDataPermissionsRejectsSymlinkAndProtectsToken(t *testing.T) {
	data := t.TempDir()
	tokenPath := filepath.Join(data, "token.json")
	if err := os.WriteFile(tokenPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := secureDataPermissions(data); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("token mode = %o, want 600", got)
	}
	if err := os.Symlink(tokenPath, filepath.Join(data, "unsafe-link")); err != nil {
		t.Fatal(err)
	}
	if err := secureDataPermissions(data); err == nil {
		t.Fatal("data directory symlink should be rejected")
	}
}
