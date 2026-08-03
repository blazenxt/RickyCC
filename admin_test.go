package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Recent Users rows: one tap-to-manage button per user with stable callback
// data, a banned marker, a name fallback, and bounded length.
func TestAdmRecentUserButtons(t *testing.T) {
	users := []User{
		{ID: 11, Name: "Aarav"},
		{ID: 22, Name: "  "},                                    // blank → fallback
		{ID: 33, Name: "BannedBandit", Banned: true},            // 🚫 marker
		{ID: 44, Name: "AVeryLongDisplayNameThatMustBeTrimmed"}, // truncated
	}

	rows := admRecentUserButtons(users)
	if len(rows) != len(users) {
		t.Fatalf("expected %d rows, got %d", len(users), len(rows))
	}
	for i, row := range rows {
		if len(row) != 1 {
			t.Fatalf("row %d should have exactly one button", i)
		}
		want := "admu.view."
		if !strings.HasPrefix(row[0].CallbackData, want) {
			t.Fatalf("row %d callback %q lacks %q prefix", i, row[0].CallbackData, want)
		}
		if id := strings.TrimPrefix(row[0].CallbackData, want); id != strconv.FormatInt(users[i].ID, 10) {
			t.Fatalf("row %d callback id %q, want %q", i, id, users[i].ID)
		}
	}

	if !strings.HasPrefix(rows[0][0].Text, "👤 Aarav") {
		t.Fatalf("normal row: %q", rows[0][0].Text)
	}
	if rows[1][0].Text != "👤 User 22" {
		t.Fatalf("blank name fallback: %q", rows[1][0].Text)
	}
	if !strings.HasPrefix(rows[2][0].Text, "🚫 BannedBandit") {
		t.Fatalf("banned marker: %q", rows[2][0].Text)
	}
	// "👤 " prefix + ≤18-rune name → never a giant button label
	if got := len([]rune(rows[3][0].Text)); got > 21 {
		t.Fatalf("long name not trimmed: %d runes %q", got, rows[3][0].Text)
	}
	if len(admRecentUserButtons(nil)) != 0 {
		t.Fatal("nil users should produce no rows")
	}
}

// User manage card: ban/unban flip by state, reset+delete always wired,
// and the card body shows the essentials.
func TestAdminUserCardViewButtons(t *testing.T) {
	u := &User{
		ID:            5,
		Name:          "Test User",
		Username:      "tester",
		ReferredUsers: []int64{1, 2, 3, 4, 5},
		Claims:        1,
		JoinedAt:      time.Now(),
	}

	text, kb := adminUserCardView(u)
	if !strings.Contains(text, "<code>5</code>") || !strings.Contains(text, "Test User") || !strings.Contains(text, "@tester") {
		t.Fatalf("card text missing identity bits: %q", text)
	}
	if !strings.Contains(text, "Referrals: <b>5</b>") {
		t.Fatalf("card text missing referral count: %q", text)
	}

	var callbacks []string
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			callbacks = append(callbacks, b.CallbackData)
		}
	}
	joinedCB := strings.Join(callbacks, " ")
	for _, want := range []string{"admu.ban.5", "admu.reset.5", "admu.del.5", "admp.users"} {
		if !strings.Contains(joinedCB, want) {
			t.Fatalf("card keyboard missing %q: %s", want, joinedCB)
		}
	}
	if strings.Contains(joinedCB, "admu.unban.5") || strings.Contains(joinedCB, "admu.view.5") {
		t.Fatalf("active user card must not show unban/view: %s", joinedCB)
	}

	// Banned user: the same button flips to Unban.
	u.Banned = true
	_, kbB := adminUserCardView(u)
	var cbB []string
	for _, row := range kbB.InlineKeyboard {
		for _, b := range row {
			cbB = append(cbB, b.CallbackData)
		}
	}
	joinedB := strings.Join(cbB, " ")
	if !strings.Contains(joinedB, "admu.unban.5") || strings.Contains(joinedB, "admu.ban.5") {
		t.Fatalf("banned card should show unban only: %s", joinedB)
	}
}

// The /cancel "abort flow" is gone: every conversation prompt must carry a
// 🔙 Back button whose callback routes through admcback.* instead, and the
// phrase must have vanished from the admin sources entirely.
func TestConversationBackButtons(t *testing.T) {
	src, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("read admin.go: %v", err)
	}
	for ln, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue // comments may mention the retired flow
		}
		if strings.Contains(line, "/cancel to abort") {
			t.Fatalf("line %d still advertises /cancel — every prompt needs a 🔙 Back button now", ln+1)
		}
	}

	for target, wantView := range map[string]string{
		"users": "admcback.users", "codes": "admcback.codes",
		"fsub": "admcback.fsub", "admins": "admcback.admins",
		"settings": "admcback.settings",
	} {
		kb := admConvBackBtn(target)
		if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
			t.Fatalf("%s: expected single back button", target)
		}
		btn := kb.InlineKeyboard[0][0]
		if btn.CallbackData != wantView {
			t.Fatalf("%s: callback %q, want %q", target, btn.CallbackData, wantView)
		}
		if btn.Style != "primary" { // decorated automatically
			t.Fatalf("%s: back button style=%q, want primary", target, btn.Style)
		}
	}

	// Every parent view must have at least one prompt navigating back to it
	// (the callback literal is built at run time as "admcback."+target).
	for _, v := range []string{"users", "codes", "fsub", "admins", "settings"} {
		if !strings.Contains(string(src), `admConvBackBtn("`+v+`")`) {
			t.Fatalf("no conversation prompt navigates back to %s", v)
		}
	}
}

// Extracted section views power both the panel switch and conversation
// back-navigation, so they must render identically.
func TestAdminSectionViews(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	uText, uKb := admUsersView()
	if !strings.Contains(uText, "User Management") {
		t.Fatalf("users view text: %q", uText)
	}
	var cb []string
	for _, row := range uKb.InlineKeyboard {
		for _, b := range row {
			cb = append(cb, b.CallbackData)
		}
	}
	j := strings.Join(cb, " ")
	for _, want := range []string{"admc.finduser", "admp.recent", "admp.home"} {
		if !strings.Contains(j, want) {
			t.Fatalf("users view missing %q: %s", want, j)
		}
	}

	cText, cKb := admCodesView()
	if !strings.Contains(cText, "Card Stock") {
		t.Fatalf("codes view text: %q", cText)
	}
	var cbC []string
	for _, row := range cKb.InlineKeyboard {
		for _, b := range row {
			cbC = append(cbC, b.CallbackData)
		}
	}
	jC := strings.Join(cbC, " ")
	for _, want := range []string{"admc.addcodes", "admp.claims", "admp.clear", "admp.home"} {
		if !strings.Contains(jC, want) {
			t.Fatalf("codes view missing %q: %s", want, jC)
		}
	}
}

// The style engine keeps giving user-management actions the right roles
// (view = primary, unban = success, ban/reset/del = danger) — pinned here
// so the manage card never loses its colors.
func TestUserMgmtStyles(t *testing.T) {
	for data, want := range map[string]string{
		"admu.view.9":   "primary",
		"admu.unban.9":  "success",
		"admu.ban.9":    "danger",
		"admu.reset.9":  "danger",
		"admu.del.9":    "danger",
		"admu.delok.9":  "danger",
		"admc.finduser": "primary",
	} {
		if got := buttonStyle(gotgbot.InlineKeyboardButton{Text: "x", CallbackData: data}); got != want {
			t.Fatalf("%s: style=%q, want %q", data, got, want)
		}
	}
}

// The fsub view must render gracefully with a nil bot (tests / first boot
// before the client exists): no panics, header + per-channel rows present.
func TestAdmFsubViewNilBot(t *testing.T) {
	text, kb := admFsubView(nil)
	if !strings.Contains(text, "Force-Join Setup") {
		t.Fatalf("header missing: %q", text)
	}
	for _, row := range kb.InlineKeyboard {
		if len(row) == 0 {
			t.Fatal("empty keyboard row")
		}
	}
}

// The alerts view must render safely with a nil bot (first boot / tests):
// header, relay status and no empty keyboard rows.
func TestAdmAlertsViewNilBot(t *testing.T) {
	text, kb := admAlertsView(nil)
	if !strings.Contains(text, "Stock Alerts") {
		t.Fatalf("header missing: %q", text)
	}
	if !strings.Contains(text, "force-join channels") {
		t.Fatalf("relay status missing: %q", text)
	}
	for _, row := range kb.InlineKeyboard {
		if len(row) == 0 {
			t.Fatal("empty keyboard row")
		}
	}
}
