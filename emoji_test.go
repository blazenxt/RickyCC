package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
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

// Buttons (Bot API 9.4+): a registry emoji at the START of a label whose slot
// maps to a NUMERIC custom ID must move into IconCustomEmojiId, with the
// duplicate trimmed from the text. Everything else stays classic.
func TestPremiumizeButtons(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	mk := func() *gotgbot.InlineKeyboardMarkup {
		return &gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			{Text: "⭐ Rate us", CallbackData: "a"},
			{Text: "🎁 Claim Reward", CallbackData: "b"},
			{Text: "⭐", CallbackData: "c"}, // label is ONLY the emoji
			{Text: "Plain label", CallbackData: "d"},
		}}}
	}

	// No mapping: completely untouched.
	kb := mk()
	premiumizeButtons(kb)
	if kb.InlineKeyboard[0][0].IconCustomEmojiId != "" || kb.InlineKeyboard[0][0].Text != "⭐ Rate us" {
		t.Fatalf("no mapping must leave buttons untouched: %+v", kb.InlineKeyboard[0][0])
	}

	// star → numeric ID converts; gift → plain emoji does NOT.
	if err := setEmojiIDs(map[string]string{"star": "5511223344556677889", "gift": "🎉"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}
	kb = mk()
	premiumizeButtons(kb)
	if b := kb.InlineKeyboard[0][0]; b.IconCustomEmojiId != "5511223344556677889" || b.Text != "Rate us" {
		t.Fatalf("star button not converted: %+v", b)
	}
	if b := kb.InlineKeyboard[0][1]; b.IconCustomEmojiId != "" || b.Text != "🎁 Claim Reward" {
		t.Fatalf("plain-emoji mapping must not touch buttons: %+v", b)
	}
	if b := kb.InlineKeyboard[0][2]; b.IconCustomEmojiId != "" || b.Text != "⭐" {
		t.Fatalf("label-only emoji must never be stripped: %+v", b)
	}
	if b := kb.InlineKeyboard[0][3]; b.Text != "Plain label" || b.IconCustomEmojiId != "" {
		t.Fatalf("non-emoji label changed: %+v", b)
	}

	// Idempotent — a second pass must not strip more text or re-set icons.
	before := *kb
	premiumizeButtons(kb)
	if !reflect.DeepEqual(*kb, before) {
		t.Fatalf("premiumizeButtons not idempotent:\n%+v\n%+v", before, *kb)
	}

	// Numeric mapping on gift converts it too, trimming just the glyph.
	if err := setEmojiIDs(map[string]string{"star": "5511223344556677889", "gift": "5500000000000000099"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}
	kb = mk()
	premiumizeButtons(kb)
	if b := kb.InlineKeyboard[0][1]; b.IconCustomEmojiId != "5500000000000000099" || b.Text != "Claim Reward" {
		t.Fatalf("gift button not converted: %+v", b)
	}

	// Empty keyboard / nil markup: no panic, no change.
	var empty gotgbot.InlineKeyboardMarkup
	premiumizeButtons(&empty)
	if premiumizeButtons(nil) != nil {
		t.Fatal("nil markup should stay nil")
	}
}

// Bot API 9.4 colors: destructive = red, claim/verify CTAs = green, key
// actions = blue, everything else = "" (client default). Rules key off
// callback data (stable), with URL-button labels as fallback.
func TestApplyButtonStyles(t *testing.T) {
	cases := []struct {
		btn  gotgbot.InlineKeyboardButton
		want string
	}{
		{gotgbot.InlineKeyboardButton{Text: "🎁 Claim Reward", CallbackData: "claim"}, "success"},
		{gotgbot.InlineKeyboardButton{Text: "✅ Joined — Try Again", CallbackData: "fsj.123"}, "success"},
		{gotgbot.InlineKeyboardButton{Text: "✅ Joined — Try Again", CallbackData: "fsj"}, "success"},
		{gotgbot.InlineKeyboardButton{Text: "✅ Unban", CallbackData: "admu.unban.9"}, "success"},
		{gotgbot.InlineKeyboardButton{Text: "🚀 Open Bot", CallbackData: "stockopen"}, "success"},
		{gotgbot.InlineKeyboardButton{Text: "✅ Yes, delete them", CallbackData: "admp.clearok"}, "danger"}, // ✅ glyph, still red
		{gotgbot.InlineKeyboardButton{Text: "🗑️ Delete User", CallbackData: "admu.del.9"}, "danger"},
		{gotgbot.InlineKeyboardButton{Text: "✅ Yes, delete", CallbackData: "admu.delok.9"}, "danger"},
		{gotgbot.InlineKeyboardButton{Text: "🚫 Ban", CallbackData: "admu.ban.9"}, "danger"},
		{gotgbot.InlineKeyboardButton{Text: "🔄 Reset Claims", CallbackData: "admu.reset.9"}, "danger"},
		{gotgbot.InlineKeyboardButton{Text: "🧹 Clear All", CallbackData: "admp.fsubclear"}, "danger"},
		{gotgbot.InlineKeyboardButton{Text: "❌ Remove 2", CallbackData: "admp.admindel.2"}, "danger"},
		{gotgbot.InlineKeyboardButton{Text: "📊 My Progress", CallbackData: "progress.5"}, "primary"},
		{gotgbot.InlineKeyboardButton{Text: "🏠 Home", CallbackData: "home"}, "primary"},
		{gotgbot.InlineKeyboardButton{Text: "🔄 Refresh", CallbackData: "admp.dash"}, "primary"},
		{gotgbot.InlineKeyboardButton{Text: "➕ Add Cards", CallbackData: "admc.addcodes"}, "success"},  // create = green
		{gotgbot.InlineKeyboardButton{Text: "➕ Add Channel", CallbackData: "admc.fsubadd"}, "success"}, // create = green
		{gotgbot.InlineKeyboardButton{Text: "➕ Add Admin", CallbackData: "admc.adminadd"}, "success"},  // create = green
		{gotgbot.InlineKeyboardButton{Text: "⚡ Load Premium Set", CallbackData: "admp.emojipremium"}, "success"},
		{gotgbot.InlineKeyboardButton{Text: "📢 Join Channel 1", Url: "https://t.me/x"}, "primary"},
		{gotgbot.InlineKeyboardButton{Text: "🔗 Refer & Earn", Url: "https://t.me/share/url?url=x"}, "primary"},
		{gotgbot.InlineKeyboardButton{Text: "🆘 Support", Url: "https://t.me/help"}, "primary"},
		{gotgbot.InlineKeyboardButton{Text: "🔙 Back", CallbackData: "admp.home"}, "primary"},    // nav = blue
		{gotgbot.InlineKeyboardButton{Text: "👥 Users", CallbackData: "admp.users"}, "primary"},  // menu = blue
		{gotgbot.InlineKeyboardButton{Text: "13", CallbackData: "cap.4"}, "primary"},            // captcha answers = blue
		{gotgbot.InlineKeyboardButton{Text: "🔗 Random Link", Url: "https://x.test"}, "primary"}, // any button gets a color
		{gotgbot.InlineKeyboardButton{Text: "📢 Force-Join Setup", CallbackData: "admp.fsub"}, "primary"},
	}
	for i, c := range cases {
		if got := buttonStyle(c.btn); got != c.want {
			t.Fatalf("case %d (%q/%q): style=%q, want %q", i, c.btn.Text, c.btn.CallbackData, got, c.want)
		}
	}

	// Whole-keyboard pass + explicit styles are never overridden.
	kb := &gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
		{Text: "🎁 Claim Reward", CallbackData: "claim"},
		{Text: "preset", CallbackData: "home", Style: "danger"},
	}}}
	applyButtonStyles(kb)
	if kb.InlineKeyboard[0][0].Style != "success" {
		t.Fatalf("claim not styled: %+v", kb.InlineKeyboard[0][0])
	}
	if kb.InlineKeyboard[0][1].Style != "danger" {
		t.Fatalf("explicit style must be preserved: %+v", kb.InlineKeyboard[0][1])
	}
	if applyButtonStyles(nil) != nil {
		t.Fatal("nil markup should stay nil")
	}
}

// decorateButtons chains icons + colors: the Claim button gets BOTH its
// premium icon and the green success style in one pass.
func TestDecorateButtons(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)
	if err := setEmojiIDs(map[string]string{"gift": "5500000000000000142"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}

	kb := decorateButtons(&gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
		{Text: "🎁 Claim Reward", CallbackData: "claim"},
		{Text: "👥 Users", CallbackData: "admp.users"},
	}}})
	claim := kb.InlineKeyboard[0][0]
	if claim.IconCustomEmojiId != "5500000000000000142" || claim.Text != "Claim Reward" {
		t.Fatalf("icon decoration missing: %+v", claim)
	}
	if claim.Style != "success" {
		t.Fatalf("style decoration missing: %+v", claim)
	}
	users := kb.InlineKeyboard[0][1]
	if users.IconCustomEmojiId != "" {
		t.Fatalf("unmapped button should not get an icon: %+v", users)
	}
	if users.Style != "primary" { // every button is styled by function now
		t.Fatalf("nav button should be primary: %+v", users)
	}
}

// Every button the bot renders must get a role color — buttonStyle never
// leaves one unstyled.
func TestButtonStyleNeverEmpty(t *testing.T) {
	data := []string{
		"", "claim", "home", "cap.0", "fsj", "fsj.99", "progress.7",
		"admp.home", "admp.dash", "admp.users", "admp.codes", "admp.settings",
		"admp.bcast", "admp.admins", "admp.close", "admp.recent", "admp.claims",
		"admp.fsub", "admp.claimstoggle", "admc.logset", "admc.target",
		"admc.support", "admc.howto", "admc.emojis", "admc.finduser",
		"admp.clearok", "admp.fsubclear", "admp.admindel.2", "admu.ban.3",
		"admu.del.3", "admu.delok.3", "admu.reset.3", "admu.unban.3",
		"admc.addcodes", "admc.fsubadd", "admc.adminadd", "admp.emojipremium",
		"progress.", "admp.gibberish", "stockopen", "admp.fsubtoggle", "admp.alerts",
		"admp.alertstoggle", "admp.alertsclear", "admp.alertsdel.5", "admc.alertsadd",
	}
	valid := map[string]bool{"danger": true, "success": true, "primary": true}
	for _, d := range data {
		s := buttonStyle(gotgbot.InlineKeyboardButton{Text: "x", CallbackData: d})
		if !valid[s] {
			t.Fatalf("callback %q got unstyled %q", d, s)
		}
	}
}

// Every new (second-wave) slot's default glyph must appear VERBATIM in the
// bot's Go sources — otherwise premiumize can never find it (e.g. missing
// variation selector, wrong keycap sequence) and the slot is dead weight.
func TestSecondWaveDefaultsPresentInSource(t *testing.T) {
	newSlots := []string{
		"back", "add", "broom", "save", "play", "pause", "refresh",
		"adminlock", "find", "trash", "gear", "crown", "diamond", "home",
		"new", "receipt", "bolt", "rocket", "info", "sad", "pointup",
		"smalldiamond", "greendot", "num1", "num2", "num3", "num4",
		"skip", "cycle", "cart", "bell",
	}

	var src strings.Builder
	for _, f := range []string{"main.go", "admin.go", "fsub.go", "captcha.go", "config.go", "dbbackup.go", "utils.go", "stocknotify.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src.Write(data)
	}
	hay := src.String()

	for _, slot := range newSlots {
		def, ok := iconDefaults[slot]
		if !ok {
			t.Fatalf("slot %q missing from iconDefaults", slot)
		}
		if id, ok := premiumEmojiDefaults[slot]; !ok || !isEmojiID(id) {
			t.Fatalf("slot %q missing numeric ID in premiumEmojiDefaults", slot)
		}
		if !strings.Contains(hay, def) {
			t.Fatalf("default %q for slot %q not found anywhere in bot sources", def, slot)
		}
	}

	// Default glyphs must be unique across the whole registry — duplicates
	// would make the premiumize sweep pick a random winner per slot.
	seen := map[string]string{}
	for slot, def := range iconDefaults {
		if prev, dup := seen[def]; dup {
			t.Fatalf("default %q used by both %q and %q", def, prev, slot)
		}
		seen[def] = slot
	}
}

func TestIconPlainEmojiValues(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	// Any public/standard emoji must be accepted and rendered as-is —
	// no tg-emoji wrapper, no validation round-trip.
	if err := setEmojiIDs(map[string]string{"card": "🔥", "party": "💎✨"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}
	if got := icon("card"); got != "🔥" {
		t.Fatalf("plain emoji slot = %q, want 🔥", got)
	}
	if got := icon("party"); got != "💎✨" {
		t.Fatalf("compound plain emoji slot = %q, want 💎✨", got)
	}
	loadConfig(0, nil) // persists across reload
	if got := icon("card"); got != "🔥" {
		t.Fatalf("plain emoji did not persist: %q", got)
	}

	// Plain emoji values must be skipped by the validator (nothing to test)
	if bad := validateEmojiIDs(nil, 0, map[string]string{"card": "🔥"}); len(bad) != 0 {
		t.Fatalf("plain emoji should need no validation, got %v", bad)
	}

	// isPlainEmoji shape checks
	for _, ok := range []string{"🔥", "💎✨", "🎟️"} {
		if !isPlainEmoji(ok) {
			t.Fatalf("isPlainEmoji(%q) = false, want true", ok)
		}
	}
	for _, no := range []string{"", "5402", "5402038549988123456", "fire", "F", "ab💎"} {
		if isPlainEmoji(no) {
			t.Fatalf("isPlainEmoji(%q) = true, want false", no)
		}
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

func TestPremiumize(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	in := "📦 Stock\n✅ done\n🎉 hi"

	// No mapping: untouched
	if got := premiumize(in); got != in {
		t.Fatalf("premiumize without mappings changed text: %q", got)
	}

	// Numeric ID mapping: swap default glyph for a tg-emoji tag
	if err := setEmojiIDs(map[string]string{"box": "6203886371363364022"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}
	got := premiumize(in)
	wantTag := `<tg-emoji emoji-id="6203886371363364022">📦</tg-emoji>`
	if !strings.Contains(got, wantTag) {
		t.Fatalf("expected %s in %q", wantTag, got)
	}
	if strings.Count(got, "🎉") != 1 { // unmapped glyph stays
		t.Fatalf("unmapped emoji was altered: %q", got)
	}

	// Plain emoji mapping specifications swap the glyph as-is
	if err := setEmojiIDs(map[string]string{"ok": "🔥"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}
	if got := premiumize(in); !strings.Contains(got, "🔥") || strings.Contains(got, "✅") {
		t.Fatalf("plain-emoji premiumize failed: %q", got)
	}

	// Mixed output coming from icon() must not double-wrap
	if err := setEmojiIDs(map[string]string{"card": "5402038549988123456"}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}
	once := premiumize(icon("card"))
	twice := premiumize(once)
	if once != twice {
		t.Fatalf("premiumize is not idempotent over icon() output: %q vs %q", once, twice)
	}
	if strings.Count(twice, "<tg-emoji") != 1 {
		t.Fatalf("icon() output mangled: %q", twice)
	}
}

func TestValidateEmojiIDsEmpty(t *testing.T) {
	if bad := validateEmojiIDs(nil, 0, map[string]string{}); len(bad) != 0 {
		t.Fatalf("empty candidate should validate clean, got %v", bad)
	}
}

// The curated premium set must only reference real slots, hold well-formed
// numeric IDs and never reuse an ID twice.
func TestPremiumEmojiDefaultsIntegrity(t *testing.T) {
	if len(premiumEmojiDefaults) < 20 {
		t.Fatalf("premium set unexpectedly small: %d slots", len(premiumEmojiDefaults))
	}
	seen := map[string]string{}
	for slot, id := range premiumEmojiDefaults {
		if _, ok := iconDefaults[slot]; !ok {
			t.Fatalf("premium set maps unknown slot %q", slot)
		}
		if !isEmojiID(id) {
			t.Fatalf("premium set slot %q has malformed ID %q", slot, id)
		}
		if prev, dup := seen[id]; dup {
			t.Fatalf("ID %s used for both %q and %q", id, prev, slot)
		}
		seen[id] = slot
	}
}
