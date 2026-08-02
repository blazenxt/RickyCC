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
	"trophy":   "6206419981161211268", // 🥇
	"card":     "6206220960966646470", // 💎
	"validity": "6206118633370818254", // ⌛
	"howto":    "6206139863394162614", // 🔖
	"gift":     "6206027872121918710", // 🎁
	"next":     "6206515969385308049", // 📶
	"link":     "6206497372176913599", // 🔗
	"stats":    "6206343625232619150", // 📊
	"person":   "5258362837411045098", // 👤
	"iddot":    "6205965994528086727", // 💠
	"lock":     "6203944611119897090", // 🛡
	"ok":       "6206185428702206246", // ✅
	"err":      "6206110936789423908", // ❌
	"warn":     "6206174450765796040", // ⚠️
	"ban":      "6206396878532121864", // 🚫
	"box":      "6203886371363364022", // 📥
	"shield":   "6203958681432757304", // 🛡
	"robot":    "5276500991108214772", // 🤖
	"num":      "6206131290639439676", // 1️⃣
	"eyes":     "6206366384264320881", // 👀
	"spy":      "6206446249181189526", // 🔍
	"spark":    "6203761490894264678", // 🌟
	"fire":     "6206041890895172990", // ❤️‍🔥
	"star":     "6206312014273321181", // ⭐️
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
