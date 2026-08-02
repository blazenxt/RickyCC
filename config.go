package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Runtime bot configuration. Env vars only seed these on first boot —
// afterwards everything is edited from the admin panel and persisted in
// the local database.
var (
	// ReferralTarget is how many friends unlock a reward (plain var; admin-only writes).
	ReferralTarget = 5
	// ClaimsPaused blocks reward claims while an admin restocks, etc.
	ClaimsPaused = false

	cfgMu        sync.RWMutex
	logChatID    int64
	fsubChannels []int64
	adminIDs     []int64
	supportURL   string
	howtoText    string
	customIcons  map[string]string // icon slot -> custom emoji id (see emoji.go)
)

// defaultHowto is shown under delivered cards until an admin sets custom text.
const defaultHowto = "Copy the card and redeem it right away. " +
	"Each card works only once — do not share it with anyone."

// loadConfig loads settings from the database, seeding them from env/defaults
// on first boot, and applies them to the runtime globals.
func loadConfig(envLogChatID int64, envFsubIDs []int64) {
	fsubJSON, _ := json.Marshal(envFsubIDs)

	// Ensure the single settings row exists (seeded from env on first boot).
	// "Skip if present" is dialect-specific: SQLite OR IGNORE / Postgres ON CONFLICT.
	seedQ := `INSERT OR IGNORE INTO settings (id, log_chat_id, fsub_channels, referral_target, claims_paused, admin_ids)
		 VALUES (1, ?, ?, ?, 0, '[]')`
	if UsingPostgres() {
		seedQ = `INSERT INTO settings (id, log_chat_id, fsub_channels, referral_target, claims_paused, admin_ids)
		 VALUES (1, $1, $2, $3, 0, '[]') ON CONFLICT (id) DO NOTHING`
	}
	_, err := db.Exec(seedQ, envLogChatID, string(fsubJSON), ReferralTarget)
	if err != nil {
		log.Printf("config: failed to seed settings: %v", err)
	}

	var fsubRaw, adminsRaw, support, howto, emojisRaw string
	var target, paused int
	var logID int64
	err = db.QueryRow(
		"SELECT log_chat_id, fsub_channels, referral_target, claims_paused, admin_ids, support_url, howto_text, emoji_ids FROM settings WHERE id = 1",
	).Scan(&logID, &fsubRaw, &target, &paused, &adminsRaw, &support, &howto, &emojisRaw)
	if err != nil {
		log.Printf("config: failed to read settings: %v", err)
		return
	}

	var fsubs, admins []int64
	_ = json.Unmarshal([]byte(fsubRaw), &fsubs)
	_ = json.Unmarshal([]byte(adminsRaw), &admins)
	var icons map[string]string
	_ = json.Unmarshal([]byte(emojisRaw), &icons)

	cfgMu.Lock()
	logChatID = logID
	fsubChannels = fsubs
	adminIDs = admins
	supportURL = support
	howtoText = howto
	customIcons = icons
	cfgMu.Unlock()

	if target > 0 {
		ReferralTarget = target
	}
	ClaimsPaused = paused != 0

	log.Printf("config loaded: log=%d fsub=%v target=%d claimsPaused=%v admins=%v",
		logID, fsubs, ReferralTarget, ClaimsPaused, admins)
}

// ---------- Getters / setters ----------

func getLogChat() int64 {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return logChatID
}

func setLogChat(id int64) error {
	if _, err := db.Exec(rebind("UPDATE settings SET log_chat_id = ? WHERE id = 1"), id); err != nil {
		return fmt.Errorf("failed to save settings: %v", err)
	}
	cfgMu.Lock()
	logChatID = id
	cfgMu.Unlock()
	return nil
}

func getFsubChannels() []int64 {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return append([]int64(nil), fsubChannels...)
}

func saveFsubChannels(next []int64) error {
	data, _ := json.Marshal(next)
	if _, err := db.Exec(rebind("UPDATE settings SET fsub_channels = ? WHERE id = 1"), string(data)); err != nil {
		return fmt.Errorf("failed to save settings: %v", err)
	}
	cfgMu.Lock()
	fsubChannels = append([]int64(nil), next...)
	cfgMu.Unlock()
	return nil
}

// addFsubChannel appends a channel to the force-join list.
// Returns false if it was already present.
func addFsubChannel(id int64) (bool, error) {
	cur := getFsubChannels()
	for _, c := range cur {
		if c == id {
			return false, nil
		}
	}
	if err := saveFsubChannels(append(cur, id)); err != nil {
		return false, err
	}
	return true, nil
}

// removeFsubChannel drops a channel from the force-join list.
// Returns false if it wasn't present.
func removeFsubChannel(id int64) (bool, error) {
	cur := getFsubChannels()
	next := make([]int64, 0, len(cur))
	found := false
	for _, c := range cur {
		if c == id {
			found = true
			continue
		}
		next = append(next, c)
	}
	if !found {
		return false, nil
	}
	if err := saveFsubChannels(next); err != nil {
		return false, err
	}
	return true, nil
}

func clearFsubChannels() error {
	return saveFsubChannels([]int64{})
}

func setReferralTarget(n int) error {
	if n < 1 {
		return fmt.Errorf("target must be at least 1")
	}
	if _, err := db.Exec(rebind("UPDATE settings SET referral_target = ? WHERE id = 1"), n); err != nil {
		return fmt.Errorf("failed to save settings: %v", err)
	}
	ReferralTarget = n
	return nil
}

func setClaimsPaused(paused bool) error {
	flag := 0
	if paused {
		flag = 1
	}
	if _, err := db.Exec(rebind("UPDATE settings SET claims_paused = ? WHERE id = 1"), flag); err != nil {
		return fmt.Errorf("failed to save settings: %v", err)
	}
	ClaimsPaused = paused
	return nil
}

// ---------- Support link & how-to text ----------

func getSupportURL() string {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return supportURL
}

// setSupportURL sets the URL behind the 🆘 Support button ("" hides it).
func setSupportURL(url string) error {
	if _, err := db.Exec(rebind("UPDATE settings SET support_url = ? WHERE id = 1"), url); err != nil {
		return fmt.Errorf("failed to save support link: %v", err)
	}
	cfgMu.Lock()
	supportURL = url
	cfgMu.Unlock()
	return nil
}

// getHowtoText returns the how-to-use text shown under delivered cards.
func getHowtoText() string {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	if howtoText == "" {
		return defaultHowto
	}
	return howtoText
}

// setHowtoText stores the how-to-use text ("" restores the default).
func setHowtoText(text string) error {
	if _, err := db.Exec(rebind("UPDATE settings SET howto_text = ? WHERE id = 1"), text); err != nil {
		return fmt.Errorf("failed to save how-to text: %v", err)
	}
	cfgMu.Lock()
	howtoText = text
	cfgMu.Unlock()
	return nil
}

// ---------- Multi-admin management ----------

// isOwner reports whether the ID is the super-owner (from OWNER_ID env).
// Only the owner may manage the admin list.
func isOwner(id int64) bool { return id == OwnerID }

// isAdmin reports whether the ID may use the admin panel (owner or listed admin).
func isAdmin(id int64) bool {
	if isOwner(id) {
		return true
	}
	for _, a := range getAdminIDs() {
		if a == id {
			return true
		}
	}
	return false
}

func getAdminIDs() []int64 {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return append([]int64(nil), adminIDs...)
}

func saveAdminIDs(next []int64) error {
	data, _ := json.Marshal(next)
	if _, err := db.Exec(rebind("UPDATE settings SET admin_ids = ? WHERE id = 1"), string(data)); err != nil {
		return fmt.Errorf("failed to save admins: %v", err)
	}
	cfgMu.Lock()
	adminIDs = append([]int64(nil), next...)
	cfgMu.Unlock()
	return nil
}

// addAdminID grants panel access to a user. Returns false if already admin/owner.
func addAdminID(id int64) (bool, error) {
	if isAdmin(id) {
		return false, nil
	}
	cur := getAdminIDs()
	if err := saveAdminIDs(append(cur, id)); err != nil {
		return false, err
	}
	return true, nil
}

// removeAdminID revokes panel access. Returns false if the ID wasn't an admin.
func removeAdminID(id int64) (bool, error) {
	cur := getAdminIDs()
	next := make([]int64, 0, len(cur))
	found := false
	for _, a := range cur {
		if a == id {
			found = true
			continue
		}
		next = append(next, a)
	}
	if !found {
		return false, nil
	}
	if err := saveAdminIDs(next); err != nil {
		return false, err
	}
	return true, nil
}

// ---------- Custom emoji icons (see emoji.go) ----------

// getEmojiID returns the custom emoji ID mapped to an icon slot, if any.
func getEmojiID(slot string) (string, bool) {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	id, ok := customIcons[slot]
	return id, ok
}

// getEmojiIDs returns a copy of the whole slot->ID map.
func getEmojiIDs() map[string]string {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	out := make(map[string]string, len(customIcons))
	for k, v := range customIcons {
		out[k] = v
	}
	return out
}

// setEmojiIDs replaces the entire custom-emoji mapping at once.
func setEmojiIDs(next map[string]string) error {
	if next == nil {
		next = map[string]string{}
	}
	data, _ := json.Marshal(next)
	if _, err := db.Exec(rebind("UPDATE settings SET emoji_ids = ? WHERE id = 1"), string(data)); err != nil {
		return fmt.Errorf("failed to save custom emojis: %v", err)
	}
	cfgMu.Lock()
	customIcons = next
	cfgMu.Unlock()
	return nil
}

func clearEmojiIDs() error {
	return setEmojiIDs(map[string]string{})
}

// ---------- Helpers ----------

// notifyLogChat sends an HTML message to the configured log chat, silently
// skipping (with a log line) when none is configured.
func notifyLogChat(b *gotgbot.Bot, text string) {
	id := getLogChat()
	if id == 0 {
		log.Printf("log chat not set — message skipped: %.80s", text)
		return
	}
	if _, err := b.SendMessage(id, premiumize(text), &gotgbot.SendMessageOpts{ParseMode: "HTML"}); err != nil {
		log.Printf("failed to send to log chat %d: %v", id, err)
	}
}
