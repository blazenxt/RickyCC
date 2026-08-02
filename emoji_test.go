package main

import (
	"strings"
	"testing"
)

func TestIconDefaultsAndCustom(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil) // seeds the settings row, same as production boot

	// Default: standard Unicode emoji, no wrapping
	if got := icon("card"); got != "💳" {
		t.Fatalf("default card icon = %q, want 💳", got)
	}
	if got := icon("nonexistent-slot"); got != "" {
		t.Fatalf("unknown slot should render empty, got %q", got)
	}

	// Custom mapping: wrapped tg-emoji with the fallback inside
	if err := setEmojiIDs(map[string]string{"card": "5402038549988123456", "party": "5402123456789012345"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}
	want := `<tg-emoji emoji-id="5402038549988123456">💳</tg-emoji>`
	if got := icon("card"); got != want {
		t.Fatalf("custom card icon = %q, want %q", got, want)
	}
	if got := icon("trophy"); got != "🏆" { // unmapped stays default
		t.Fatalf("unmapped icon changed: %q", got)
	}

	// Persisted across a reload (simulates restart)
	loadConfig(0, nil)
	if got := icon("card"); got != want {
		t.Fatalf("custom emoji did not persist across reload: %q", got)
	}

	// Clearing restores defaults
	if err := clearEmojiIDs(); err != nil {
		t.Fatalf("clearEmojiIDs: %v", err)
	}
	if got := icon("card"); got != "💳" {
		t.Fatalf("clear should restore default, got %q", got)
	}
	loadConfig(0, nil)
	if len(getEmojiIDs()) != 0 {
		t.Fatal("cleared mapping should stay cleared after reload")
	}
}

func TestStripTGEmoji(t *testing.T) {
	in := `🎉 Hi <tg-emoji emoji-id="123">💳</tg-emoji> and <tg-emoji emoji-id="456">🏆</tg-emoji>!`
	want := "🎉 Hi 💳 and 🏆!"
	if got := stripTGEmoji(in); got != want {
		t.Fatalf("stripTGEmoji = %q, want %q", got, want)
	}
	// Plain text passes through unchanged
	if got := stripTGEmoji("no tags here 🎁"); got != "no tags here 🎁" {
		t.Fatalf("plain text changed: %q", got)
	}
}

func TestEmojiValidationInput(t *testing.T) {
	valid := []string{"5402038549988123456", "12345", "99999999999999999999"}
	for _, v := range valid {
		if !isEmojiID(v) {
			t.Fatalf("isEmojiID(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "123", "abc123", "5402x", "5402 3", "🔥", "123456789012345678901234567890"}
	for _, v := range invalid {
		if isEmojiID(v) {
			t.Fatalf("isEmojiID(%q) = true, want false", v)
		}
	}
}

func TestEmojiSlotListCoversRegistry(t *testing.T) {
	list := emojiSlotList()
	for name := range iconDefaults {
		if !strings.Contains(list, name) {
			t.Fatalf("slot list missing %q", name)
		}
	}
}

func TestValidateEmojiIDsEmpty(t *testing.T) {
	if bad := validateEmojiIDs(nil, 0, map[string]string{}); len(bad) != 0 {
		t.Fatalf("empty candidate should validate clean, got %v", bad)
	}
}
