package main

import (
	"regexp"
	"strings"
	"testing"
)

var menuCmdRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// Menu entries must satisfy Telegram's command-name rules and stay unique.
func TestMenuCommandsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range userMenuCommands() {
		if !menuCmdRe.MatchString(c.Command) {
			t.Fatalf("invalid user command %q", c.Command)
		}
		if seen[c.Command] {
			t.Fatalf("duplicate command %q", c.Command)
		}
		seen[c.Command] = true
		if strings.TrimSpace(c.Description) == "" || len(c.Description) > 256 {
			t.Fatalf("bad description for %q", c.Command)
		}
	}
}

// The admin menu must be a strict superset of the public menu — same public
// commands first, then the toolset — and every name must be valid.
func TestAdminMenuCommands(t *testing.T) {
	pub := userMenuCommands()
	adm := adminMenuCommands()

	if len(adm) <= len(pub) {
		t.Fatalf("admin menu should extend the public one: %d vs %d", len(adm), len(pub))
	}
	for i, c := range pub {
		if adm[i] != c {
			t.Fatalf("admin menu entry %d = %+v, want %+v", i, adm[i], c)
		}
	}
	seen := map[string]bool{}
	for _, c := range adm {
		if !menuCmdRe.MatchString(c.Command) {
			t.Fatalf("invalid admin command %q", c.Command)
		}
		if seen[c.Command] {
			t.Fatalf("duplicate command %q", c.Command)
		}
		seen[c.Command] = true
	}
}

// The owner is always in the admin-menu recipients, never duplicated.
func TestMenuAdminChatIDs(t *testing.T) {
	defer func(prev int64) { OwnerID = prev }(OwnerID)
	OwnerID = 8726642457

	ids := menuAdminChatIDs()
	if len(ids) == 0 || ids[0] != OwnerID {
		t.Fatalf("owner should lead the list: %v", ids)
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate recipient %d in %v", id, ids)
		}
		seen[id] = true
	}
}
