package main

import (
	"strings"
	"testing"
	"time"
)

// Exactly five presets, all loadable via the embedded tzdata (Alpine has no
// system zoneinfo — this is why `time/tzdata` is imported).
func TestTZPresetsLoadable(t *testing.T) {
	if len(tzPresets) != 5 {
		t.Fatalf("want exactly 5 presets, got %d", len(tzPresets))
	}
	for _, p := range tzPresets {
		if _, err := time.LoadLocation(p.Name); err != nil {
			t.Fatalf("preset %q must load: %v", p.Name, err)
		}
		if p.Flag == "" || p.Label == "" {
			t.Fatalf("preset %+v missing button fields", p)
		}
		if len("admp.tzset."+p.Name) > 64 {
			t.Fatalf("callback_data for %q exceeds Telegram's 64-byte limit", p.Name)
		}
	}
}

func TestTZPersistenceAndFormatting(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)
	defer applyTZ("UTC")

	if got := getTZName(); got != "UTC" {
		t.Fatalf("fresh DB default = %q, want UTC", got)
	}

	if err := setTZName("Asia/Kolkata"); err != nil {
		t.Fatalf("setTZName: %v", err)
	}
	if got := getTZName(); got != "Asia/Kolkata" {
		t.Fatalf("after set: %q", got)
	}

	loadConfig(0, nil) // simulate a restart re-read
	if got := getTZName(); got != "Asia/Kolkata" {
		t.Fatalf("zone must survive a config reload, got %q", got)
	}

	// 10:00 UTC = 15:30 in Kolkata (UTC+5:30)
	instant := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	if got := fmtBotTime(instant, "15:04"); got != "15:30" {
		t.Fatalf("Kolkata render = %q, want 15:30", got)
	}
	applyTZ("UTC")
	if got := fmtBotTime(instant, "15:04"); got != "10:00" {
		t.Fatalf("UTC render = %q, want 10:00", got)
	}
}

func TestTZRejectsUnknown(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)
	defer applyTZ("UTC")

	before := getTZName()
	if err := setTZName("Mars/Olympus_Mons"); err == nil {
		t.Fatal("unknown zone must be rejected")
	}
	if got := getTZName(); got != before {
		t.Fatalf("rejected set must not change the zone: %q → %q", before, got)
	}
}

func TestTZInvalidStoredFallsBackToUTC(t *testing.T) {
	defer applyTZ("UTC")

	applyTZ("Bogus/Zone")
	if got := getTZName(); got != "UTC" {
		t.Fatalf("invalid stored zone must fall back to UTC, got %q", got)
	}
	if loc := getTZLocation(); loc != time.UTC {
		t.Fatalf("location should be time.UTC, got %v", loc)
	}
}

// The presets view: 5 zone buttons + back, current zone ticked, every
// callback within Telegram's 64-byte data limit.
func TestAdmTZView(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)
	defer applyTZ("UTC")

	text, kb := admTZView()
	if !strings.Contains(text, "Current:") {
		t.Fatal("view should state the current zone")
	}
	if got, want := len(kb.InlineKeyboard), len(tzPresets)+1; got != want {
		t.Fatalf("rows = %d, want %d (presets + back)", got, want)
	}
	ticked := 0
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if strings.Contains(btn.Text, "✅") {
				ticked++
			}
			if name, ok := strings.CutPrefix(btn.CallbackData, "admp.tzset."); ok {
				if _, ok := tzPresetByName(name); !ok {
					t.Fatalf("button offers unknown zone %q", name)
				}
			}
		}
	}
	if ticked != 1 {
		t.Fatalf("exactly the current zone must be ticked, got %d ticks", ticked)
	}
}

func TestAdmBotSettingsView(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)
	defer applyTZ("UTC")

	text, kb := admBotSettingsView()
	for _, want := range []string{"Bot Settings", "Time zone:", "UTC"} {
		if !strings.Contains(text, want) {
			t.Fatalf("view missing %q: %s", want, text)
		}
	}
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected Time Zone + Settings back buttons, got %d rows", len(kb.InlineKeyboard))
	}
}
