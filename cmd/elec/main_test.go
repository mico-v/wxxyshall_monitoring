package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
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

func TestTargetSchedulesHonorPerTargetInterval(t *testing.T) {
	override := 15
	cfg := &config.Config{
		PollIntervalMin: 60,
		Targets: []config.Target{
			{Campus: "A", Building: "B", Room: "1"},
			{Campus: "A", Building: "B", Room: "2", PollIntervalMin: &override},
		},
	}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	schedules := make(map[string]targetSchedule)
	reconcileTargetSchedules(schedules, cfg, now)
	due := dueTargets(schedules, cfg, now)
	if len(due) != 2 {
		t.Fatalf("initial due targets = %d, want 2", len(due))
	}
	markTargetsScheduled(schedules, due, now)

	due = dueTargets(schedules, cfg, now.Add(15*time.Minute))
	if len(due) != 1 || due[0].Room != "2" {
		t.Fatalf("15 minute due targets = %+v", due)
	}
	markTargetsScheduled(schedules, due, now.Add(15*time.Minute))
	due = dueTargets(schedules, cfg, now.Add(60*time.Minute))
	if len(due) != 2 {
		t.Fatalf("60 minute due targets = %d, want 2", len(due))
	}
}

func TestTargetScheduleChangeBecomesDueImmediately(t *testing.T) {
	override := 30
	target := config.Target{Campus: "A", Building: "B", Room: "1", PollIntervalMin: &override}
	cfg := &config.Config{PollIntervalMin: 60, Targets: []config.Target{target}}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	schedules := make(map[string]targetSchedule)
	reconcileTargetSchedules(schedules, cfg, now)
	markTargetsScheduled(schedules, dueTargets(schedules, cfg, now), now)

	changed := 5
	cfg.Targets[0].PollIntervalMin = &changed
	reconcileTargetSchedules(schedules, cfg, now.Add(time.Minute))
	if due := dueTargets(schedules, cfg, now.Add(time.Minute)); len(due) != 1 {
		t.Fatalf("changed interval due targets = %d, want 1", len(due))
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
