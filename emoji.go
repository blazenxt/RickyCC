package main

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Custom-emoji icon system.
//
// Every symbolic icon in the bot's message bodies has a slot name and a
// standard (Unicode) default. The owner can map any slot to a CUSTOM EMOJI
// (Telegram premium emoji) from the admin panel — set IDs are persisted in
// the settings table and wrapped as <tg-emoji emoji-id="…">fallback</tg-emoji>
// in HTML messages and photo captions.
//
// Hard limits of Telegram, respected throughout:
//   - inline/keyboard BUTTON text cannot render custom emoji → buttons keep
//     the standard fallback automatically,
//   - a bot may only send custom emoji from emoji packs IT owns
//     (@BotFather → /newemojipack → this bot) — so every pasted ID is
//     validated with a live test-send before it is saved,
//   - if a send ever fails, stripTGEmoji() downgrades messages back to the
//     standard fallbacks so delivery can never wedge.

// premiumEmojiDefaults is a curated one-tap premium look, sourced from
// popular public premium emoji packs (owner-supplied 124-ID pool).
//
// Bots may only send PUBLIC custom emoji when they hold an extra username
// bought on Fragment. That's why this set is enabled exclusively through the
// admin panel's "⚡ Load Premium Set" action, which first probes with a live
// test-send — applying it on an unsupported bot is impossible, so message
// delivery can never break.
var premiumEmojiDefaults = map[string]string{
	"party":    "6206378324273403309", // 🎉
	"wave":     "6206212212118263684", // 🙂
	"trophy":   "6206419981161211268", // 🥇
	"card":     "6206220960966646470", // 💎
	"validity": "6206118633370818254", // ⌛
	"howto":    "6206139863394162614", // 🔖
	"gift":     "6206027872121918710", // 🎁
	"users":    "5258362837411045098", // 👤
	"next":     "6206515969385308049", // 📶
	"link":     "6206497372176913599", // 🔗
	"stats":    "6206343625232619150", // 📊
	"person":   "5397971251878732060", // 🧸
	"iddot":    "6205965994528086727", // 💠
	"id":       "6206190608432764318", // 📌
	"lock":     "6203944611119897090", // 🛡
	"ok":       "6206185428702206246", // ✅
	"err":      "6206110936789423908", // ❌
	"warn":     "6206174450765796040", // ⚠️
	"ban":      "6206396878532121864", // 🚫
	"box":      "6203886371363364022", // 📥
	"ticket":   "6205984948218762570", // 🛍
	"cal":      "6206325217002788818", // ➡️
	"clock":    "6204251568137574946", // 🔄
	"name":     "6204162490515855272", // ✏️
	"shield":   "6203958681432757304", // 🛡
	"wrench":   "5323284841502883276", // 🛞
	"robot":    "5276500991108214772", // 🤖
	"math":     "6206375377925839184", // ➕
	"num":      "6206131290639439676", // 1️⃣
	"eyes":     "6206366384264320881", // 👀
	"spy":      "6206446249181189526", // 🔍
	"bullet":   "6206141323683042874", // ▶️
	"spark":    "6203761490894264678", // 🌟
	"fire":     "6206041890895172990", // ❤️‍🔥
	"star":     "6206312014273321181", // ⭐️
	"mega":     "6206080502651164081", // 📣
	"log":      "6206495649895028694", // 💬
	"target":   "6203738495639360972", // 💯
	"sos":      "6206306186002698906", // ❓
	"palette":  "5201830300312683111", // 💜

	// Second wave — owner-supplied IDs for every previously-uncovered
	// Unicode emoji (back buttons, admin actions, help keycaps, ...).
	"back":         "5352759161945867747", // 🔙
	"add":          "5235472087652510235", // ➕
	"broom":        "5280603307646149925", // 🧹
	"save":         "5462956611033117422", // 💾
	"play":         "5208607440878197365", // ▶️
	"pause":        "5042036407137207122", // ⏸️
	"refresh":      "5226702984204797593", // 🔄
	"adminlock":    "5197288647275071607", // 🔐
	"find":         "5463352748751753567", // 🔍
	"trash":        "5372825386591732174", // 🗑️
	"gear":         "5420155432272438703", // ⚙️
	"crown":        "6172745002314118594", // 👑
	"diamond":      "5280858699286471614", // 💎
	"home":         "5346175999283308805", // 🏠
	"new":          "6052910771896064432", // 🆕
	"receipt":      "5444856076954520455", // 🧾
	"bolt":         "6253483549890973859", // ⚡
	"rocket":       "6337086239358851786", // 🚀
	"info":         "5334544901428229844", // ℹ️
	"sad":          "5278552623971049028", // 😔
	"pointup":      "6242033978828658746", // 👆
	"smalldiamond": "5972165824817925650", // 🔸
	"greendot":     "5981066684977384749", // 🟢
	"num1":         "5305763715692377402", // 1️⃣
	"num2":         "5307907239380528763", // 2️⃣
	"num3":         "5305783000095537258", // 3️⃣
	"num4":         "5305255243104138538", // 4️⃣
}

var iconDefaults = map[string]string{
	"party":    "🎉",
	"wave":     "👋",
	"trophy":   "🏆",
	"card":     "💳",
	"validity": "⏳",
	"howto":    "📖",
	"gift":     "🎁",
	"users":    "👥",
	"next":     "📶",
	"link":     "🔗",
	"stats":    "📊",
	"person":   "👤",
	"iddot":    "🔹",
	"id":       "🆔",
	"lock":     "🔒",
	"ok":       "✅",
	"err":      "❌",
	"warn":     "⚠️",
	"ban":      "🚫",
	"box":      "📦",
	"ticket":   "🎟️",
	"cal":      "📅",
	"clock":    "🕒",
	"name":     "📛",
	"shield":   "🔰",
	"wrench":   "🛠",
	"robot":    "🤖",
	"math":     "🧮",
	"num":      "🔢",
	"eyes":     "👀",
	"spy":      "🕵️",
	"bullet":   "▫️",
	"spark":    "✨",
	"fire":     "🔥",
	"star":     "⭐",
	"mega":     "📢",
	"log":      "🪵",
	"target":   "🎯",
	"sos":      "🆘",
	"palette":  "🎨",

	// Second wave — defaults MUST byte-match the literals used in source
	// (variation selectors included), otherwise premiumize can't find them.
	"back":         "🔙",
	"add":          "➕",
	"broom":        "🧹",
	"save":         "💾",
	"play":         "▶️",
	"pause":        "⏸️",
	"refresh":      "🔄",
	"adminlock":    "🔐",
	"find":         "🔍",
	"trash":        "🗑️",
	"gear":         "⚙️",
	"crown":        "👑",
	"diamond":      "💎",
	"home":         "🏠",
	"new":          "🆕",
	"receipt":      "🧾",
	"bolt":         "⚡",
	"rocket":       "🚀",
	"info":         "ℹ️",
	"sad":          "😔",
	"pointup":      "👆",
	"smalldiamond": "🔸",
	"greendot":     "🟢",
	"num1":         "1️⃣",
	"num2":         "2️⃣",
	"num3":         "3️⃣",
	"num4":         "4️⃣",
}

var tgEmojiTagRe = regexp.MustCompile(`<tg-emoji emoji-id="\d+">.*?</tg-emoji>`)

// premiumize replaces remaining raw Unicode emoji occurrences in a message
// body with their configured premium counterparts. Slots mapped to numeric
// IDs become <tg-emoji> tags; slots mapped to plain emojis swap the glyph
// directly. Idempotent: text already inside <tg-emoji> tags is treated as
// atomic and never re-processed. Buttons are struct fields — never passed
// through here, per design.
func premiumize(s string) string {
	mapping := getEmojiIDs()
	if len(mapping) == 0 {
		return s
	}

	// Only segments OUTSIDE existing tg-emoji tags are transformed, so we can
	// never nest a new tag inside icon() output (which would break entities).
	parts := tgEmojiTagRe.Split(s, -1)
	tags := tgEmojiTagRe.FindAllString(s, -1)

	var sb strings.Builder
	for i, part := range parts {
		for slot, v := range mapping {
			if v == "" {
				continue
			}
			def, ok := iconDefaults[slot]
			if !ok || def == "" {
				continue
			}
			if isEmojiID(v) {
				part = strings.ReplaceAll(part, def, `<tg-emoji emoji-id="`+v+`">`+def+`</tg-emoji>`)
			} else {
				part = strings.ReplaceAll(part, def, v)
			}
		}
		sb.WriteString(part)
		if i < len(tags) {
			sb.WriteString(tags[i])
		}
	}
	return sb.String()
}

// emojiSlotList renders the slot reference for the admin panel:
// "party 🎉 · trophy 🏆 · card 💳 …"
func emojiSlotList() string {
	names := make([]string, 0, len(iconDefaults))
	for n := range iconDefaults {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	for i, n := range names {
		if i > 0 {
			sb.WriteString("  ·  ")
		}
		fmt.Fprintf(&sb, "<code>%s</code> %s", n, iconDefaults[n])
	}
	return sb.String()
}

// isEmojiID checks a pasted value looks like a numeric custom_emoji_id.
func isEmojiID(s string) bool {
	if len(s) < 5 || len(s) > 25 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isPlainEmoji checks a value is a short emoji literal (e.g. "🔥", "💎✨")
// rather than a custom emoji ID. Max ~8 code points keeps panels tidy.
func isPlainEmoji(s string) bool {
	if s == "" || isEmojiID(s) {
		return false
	}
	r := []rune(s)
	if len(r) > 8 {
		return false
	}
	for _, c := range r {
		if c < 0x80 { // plain ASCII text/numbers are not emoji art
			return false
		}
	}
	return true
}

// icon returns the rendering of a slot for HTML message bodies / captions:
// a <tg-emoji> wrapper for mapped custom emoji IDs, a mapped literal emoji
// as-is, otherwise the standard Unicode default.
func icon(name string) string {
	def, ok := iconDefaults[name]
	if !ok {
		return ""
	}
	if v, set := getEmojiID(name); set && v != "" {
		if isEmojiID(v) {
			return fmt.Sprintf(`<tg-emoji emoji-id="%s">%s</tg-emoji>`, v, def)
		}
		return v // owner mapped this slot to a plain (public) emoji
	}
	return def
}

var tgEmojiRe = regexp.MustCompile(`<tg-emoji emoji-id="\d+">(.*?)</tg-emoji>`)

// stripTGEmoji replaces every custom-emoji tag with its inner fallback emoji,
// producing a message that any client/account combination will accept.
func stripTGEmoji(s string) string {
	return tgEmojiRe.ReplaceAllString(s, "$1")
}

// validateEmojiIDs test-sends the candidate numeric custom-emoji IDs to the
// chat and returns the slots Telegram rejected (e.g. pack not owned by the
// bot and no Fragment username). Plain-literal emoji values never need
// validation and are ignored here.
func validateEmojiIDs(b *gotgbot.Bot, chatID int64, candidate map[string]string) map[string]string {
	bad := map[string]string{}
	if len(candidate) == 0 {
		return bad
	}

	render := func(slots []string) string {
		var sb strings.Builder
		for _, s := range slots {
			fmt.Fprintf(&sb, "%s: %s\n", s, `<tg-emoji emoji-id="`+candidate[s]+`">`+iconDefaults[s]+`</tg-emoji>`)
		}
		return sb.String()
	}

	slots := make([]string, 0, len(candidate))
	for s := range candidate {
		if isEmojiID(candidate[s]) { // literal emojis (🔥…) never need testing
			slots = append(slots, s)
		}
	}
	sort.Strings(slots)
	if len(slots) == 0 {
		return bad
	}

	if _, err := b.SendMessage(chatID, render(slots), &gotgbot.SendMessageOpts{ParseMode: "HTML"}); err == nil {
		log.Printf("custom emoji validation: all %d IDs accepted", len(slots))
		return bad
	}

	// At least one ID is unusable — find out exactly which.
	for _, s := range slots {
		if _, err := b.SendMessage(chatID, render([]string{s}), &gotgbot.SendMessageOpts{ParseMode: "HTML"}); err != nil {
			bad[s] = candidate[s]
		}
	}
	log.Printf("custom emoji validation: %d bad of %d", len(bad), len(slots))
	return bad
}

// premiumizeButtons upgrades inline buttons to the premium look introduced
// in Bot API 9.4: when a button label STARTS with a registry emoji whose
// slot is mapped to a numeric custom emoji ID, that glyph moves from the
// text into the button's IconCustomEmojiId and the duplicate is trimmed
// from the label. Everything else — plain-emoji mappings, unmapped slots,
// non-emoji text — is left untouched, falling back to the classic
// standard-emoji button exactly like before. A button whose label is ONLY
// the emoji is never stripped (Telegram needs visible label text).
// In-place; returns kb for chaining.
func premiumizeButtons(kb *gotgbot.InlineKeyboardMarkup) *gotgbot.InlineKeyboardMarkup {
	if kb == nil {
		return kb
	}
	mapping := getEmojiIDs()
	if len(mapping) == 0 {
		return kb
	}

	defToID := map[string]string{}
	for slot, v := range mapping {
		if def, ok := iconDefaults[slot]; ok && def != "" && isEmojiID(v) {
			defToID[def] = v
		}
	}
	if len(defToID) == 0 {
		return kb
	}

	for ri := range kb.InlineKeyboard {
		for bi := range kb.InlineKeyboard[ri] {
			btn := &kb.InlineKeyboard[ri][bi]
			if btn.IconCustomEmojiId != "" {
				continue // already has an icon — never double-set
			}
			for def, id := range defToID {
				if !strings.HasPrefix(btn.Text, def) {
					continue
				}
				if rest := strings.TrimSpace(btn.Text[len(def):]); rest != "" {
					btn.Text = rest
					btn.IconCustomEmojiId = id
				}
				break
			}
		}
	}
	return kb
}

// buttonStyle assigns a Bot API 9.4 button color by the button's FUNCTION,
// keyed off stable callback data. EVERY button the bot sends gets a role
// color — "" is never returned:
//
//	danger  (red)    — destructive ops: deletes, bans, resets, wipes,
//	                   even when the label says "✅ Yes, delete"
//	success (green)  — positive CTAs: claim rewards, verify join, unban,
//	                   and create/add actions (cards, channels, admins,
//	                   loading the premium set)
//	primary (blue)   — everything else: navigation, menus, settings,
//	                   info, captcha answers, links
//
// Explicit pre-set styles are never touched by the caller.
func buttonStyle(btn gotgbot.InlineKeyboardButton) string {
	d := btn.CallbackData

	// 1) Destructive — red must never be ambiguous, even when the label
	//    starts with a ✅ (e.g. "Yes, delete").
	switch {
	case strings.HasPrefix(d, "admp.clearok"), // wipe claimed cards
		strings.HasPrefix(d, "admp.fsubclear"), // wipe all fsub channels
		strings.HasPrefix(d, "admp.admindel."), // remove an admin
		strings.HasPrefix(d, "admu.ban."),      // ban a user
		strings.HasPrefix(d, "admu.del."),      // delete user
		strings.HasPrefix(d, "admu.delok."),    // delete user, confirmed
		strings.HasPrefix(d, "admu.reset."):    // reset a user's claims
		return "danger"
	}

	// 2) Positive CTAs — claiming, verifying, unbanning, CREATING things.
	if d == "claim" || d == "fsj" || strings.HasPrefix(d, "fsj.") ||
		strings.HasPrefix(d, "admu.unban.") ||
		d == "admc.addcodes" || d == "admc.fsubadd" || d == "admc.adminadd" ||
		d == "admp.emojipremium" {
		return "success"
	}

	// 3) Everything else is a primary-style action: navigation, menus,
	//    settings, info, links, captcha answers.
	return "primary"
}

// applyButtonStyles colors every button via buttonStyle (Bot API 9.4 style
// field). Explicit pre-set styles are never overridden. In-place, chainable.
func applyButtonStyles(kb *gotgbot.InlineKeyboardMarkup) *gotgbot.InlineKeyboardMarkup {
	if kb == nil {
		return kb
	}
	for ri := range kb.InlineKeyboard {
		for bi := range kb.InlineKeyboard[ri] {
			btn := &kb.InlineKeyboard[ri][bi]
			if btn.Style == "" {
				btn.Style = buttonStyle(*btn)
			}
		}
	}
	return kb
}

// decorateButtons is the one-stop call applied to every keyboard the bot
// sends: premium custom-emoji icons (when loaded) + 9.4 colors — always.
func decorateButtons(kb *gotgbot.InlineKeyboardMarkup) *gotgbot.InlineKeyboardMarkup {
	return applyButtonStyles(premiumizeButtons(kb))
}

// preloadPremiumEmojiSet keeps the curated premium set applied on boot.
// It MERGES missing slots only — any value the owner already configured
// (custom IDs or plain emojis) is preserved untouched, so this is safe to
// run on every restart: fresh deploys get the full set, older installs
// gain newly-added slots automatically. A live probe (deleted right
// after) first proves the bot may send public-pack custom emoji — without
// it, unusable IDs would wedge message delivery.
func preloadPremiumEmojiSet(b *gotgbot.Bot, ownerID int64) {
	existing := getEmojiIDs()
	missing := map[string]string{}
	for slot, id := range premiumEmojiDefaults {
		if _, ok := existing[slot]; !ok {
			missing[slot] = id
		}
	}
	if len(missing) == 0 {
		log.Printf("premium emoji: full curated set (%d slots) already active", len(premiumEmojiDefaults))
		return
	}
	if ownerID == 0 {
		log.Printf("premium emoji pre-load skipped (%d slots ready): OWNER_ID not set — use /admin → ⚡ Load Premium Set anytime", len(missing))
		return
	}

	var sb strings.Builder
	for _, slot := range []string{"party", "robot", "gift"} {
		fmt.Fprintf(&sb, "<tg-emoji emoji-id=\"%s\">%s</tg-emoji> ", premiumEmojiDefaults[slot], iconDefaults[slot])
	}
	probe, err := b.SendMessage(ownerID, sb.String(), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	if err != nil {
		log.Printf("premium emoji pre-load skipped (%d slots ready): bot can't send public custom emoji yet (%v) — needs a Fragment username / Premium owner, then /admin → ⚡ Load Premium Set", len(missing), err)
		return
	}
	_, _ = b.DeleteMessage(ownerID, probe.MessageId, nil)

	merged := getEmojiIDs() // fresh copy — keeps every owner-made mapping
	for slot, id := range missing {
		merged[slot] = id
	}
	if err := setEmojiIDs(merged); err != nil {
		log.Printf("premium emoji pre-load failed: %v", err)
		return
	}
	log.Printf("premium emoji set active: %d slots (%d newly added on this boot)", len(merged), len(missing))
}
