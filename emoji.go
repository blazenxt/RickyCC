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

// isEmojiID checks a pasted custom_emoji_id is plausibly a numeric ID.
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

// icon returns the rendering of a slot for HTML message bodies / captions:
// the custom <tg-emoji> wrapper when the owner mapped this slot, otherwise
// the standard Unicode emoji.
func icon(name string) string {
	def, ok := iconDefaults[name]
	if !ok {
		return ""
	}
	if id, set := getEmojiID(name); set && id != "" {
		return fmt.Sprintf(`<tg-emoji emoji-id="%s">%s</tg-emoji>`, id, def)
	}
	return def
}

var tgEmojiRe = regexp.MustCompile(`<tg-emoji emoji-id="\d+">(.*?)</tg-emoji>`)

// stripTGEmoji replaces every custom-emoji tag with its inner fallback emoji,
// producing a message that any client/account combination will accept.
func stripTGEmoji(s string) string {
	return tgEmojiRe.ReplaceAllString(s, "$1")
}

// validateEmojiIDs test-sends the candidate custom emoji to the chat and
// returns the slots Telegram rejected (e.g. pack not owned by the bot).
// A nil/empty result means every ID is usable.
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
		slots = append(slots, s)
	}
	sort.Strings(slots)

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
