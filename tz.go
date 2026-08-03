package main

import (
	"fmt"
	"log"
	"sync"
	"time"
	_ "time/tzdata" // embedded zoneinfo — LoadLocation works on tzdata-less Alpine images

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// ─────────────────────────────────────────────────────────────────────────────
// Display time zone — /admin → 🤖 Bot Settings → 🕐 Time Zone.
//
// The engine stores everything in UTC underneath; this setting controls how
// timestamps are SHOWN inside the admin panel (dashboard refresh time,
// claims list, joined dates) and on DB-backup captions. It persists in
// settings.tz and is re-applied by loadConfig on every boot.
//
// NOTE: this file imports `time/tzdata` so time.LoadLocation works even on
// Alpine containers that ship no system zoneinfo files.
// ─────────────────────────────────────────────────────────────────────────────

var (
	tzMu   sync.RWMutex
	tzLoc  = time.UTC
	tzProp = "UTC"
)

// tzPreset is one tap-to-select time-zone button.
type tzPreset struct {
	Flag  string // button flag emoji
	Label string // human label shown on the button
	Name  string // IANA location (stored in the DB)
}

// tzPresets — the five time-zone buttons (top global picks for this bot's
// audience; extend the slice to offer more).
var tzPresets = []tzPreset{
	{"🇮🇳", "India (IST, UTC+5:30)", "Asia/Kolkata"},
	{"🌐", "UTC", "UTC"},
	{"🇦🇪", "Dubai (GST, UTC+4)", "Asia/Dubai"},
	{"🇬🇧", "London (UK)", "Europe/London"},
	{"🇺🇸", "New York (US Eastern)", "America/New_York"},
}

func tzPresetByName(name string) (tzPreset, bool) {
	for _, p := range tzPresets {
		if p.Name == name {
			return p, true
		}
	}
	return tzPreset{}, false
}

// applyTZ caches the location behind the stored name; anything unreadable
// falls back to UTC with a log line instead of crashing the panel.
func applyTZ(name string) {
	loc, err := time.LoadLocation(name)
	if err != nil {
		if name != "" && name != "UTC" {
			log.Printf("tz: unknown stored zone %q — falling back to UTC", name)
		}
		loc = time.UTC
		name = "UTC"
	}
	tzMu.Lock()
	tzLoc, tzProp = loc, name
	tzMu.Unlock()
}

func getTZName() string {
	tzMu.RLock()
	defer tzMu.RUnlock()
	return tzProp
}

func getTZLocation() *time.Location {
	tzMu.RLock()
	defer tzMu.RUnlock()
	return tzLoc
}

// setTZName persists a NEW display zone. Only presets are accepted — the
// panel is the single writer and its buttons are exactly the presets.
func setTZName(name string) error {
	if _, ok := tzPresetByName(name); !ok {
		return fmt.Errorf("unsupported time zone %q — pick one of the preset buttons", name)
	}
	if _, err := db.Exec(rebind("UPDATE settings SET tz = ? WHERE id = 1"), name); err != nil {
		return err
	}
	applyTZ(name)
	log.Printf("admin set time zone to %s", name)
	return nil
}

// fmtBotTime formats t in the configured display zone.
func fmtBotTime(t time.Time, layout string) string {
	return t.In(getTZLocation()).Format(layout)
}

// botNow is the current instant shifted into the display zone.
func botNow() time.Time { return time.Now().In(getTZLocation()) }

// ── admin panel views ───────────────────────────────────────────────────────

// admBotSettingsView — the "🤖 Bot Settings" section inside Settings.
// Bot-level tweaks that are about the bot instance itself (not content):
// time zone today, more later.
func admBotSettingsView() (string, gotgbot.InlineKeyboardMarkup) {
	label := getTZName()
	if p, ok := tzPresetByName(getTZName()); ok {
		label = p.Label
	}
	text := fmt.Sprintf(
		"🤖 <b>Bot Settings</b>\n\n"+
			"🕐 <b>Time zone:</b> %s · <code>%s</code>\n"+
			"🕒 <b>Time right now:</b> <b>%s</b>\n\n"+
			"<i>Used for every timestamp inside the panel (dashboard, claims, joined dates) and on backup captions. Data underneath always stays UTC.</i>",
		esc(label), getTZName(), botNow().Format(admTimeFmt))
	kb := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{admBtn("🕐 Time Zone", "admp.tz")},
			{admBtn("🛠 Settings", "admp.settings")},
		},
	}
	return text, kb
}

// admTZView — five preset zones, current one marked ✅.
func admTZView() (string, gotgbot.InlineKeyboardMarkup) {
	cur := getTZName()
	var rows [][]gotgbot.InlineKeyboardButton
	for _, p := range tzPresets {
		mark := ""
		if p.Name == cur {
			mark = "  ✅"
		}
		rows = append(rows, []gotgbot.InlineKeyboardButton{
			admBtn(p.Flag+" "+p.Label+mark, "admp.tzset."+p.Name),
		})
	}
	rows = append(rows, []gotgbot.InlineKeyboardButton{
		admBtn("🔙 Bot Settings", "admp.botset"),
	})
	text := fmt.Sprintf(
		"🕐 <b>Time Zone</b>\n\n"+
			"Current: <b>%s</b>\n"+
			"Time right now: <b>%s</b>\n\n"+
			"Tap a preset — it applies instantly and persists across restarts.",
		esc(cur), botNow().Format("15:04:05"))
	return text, gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}
}
