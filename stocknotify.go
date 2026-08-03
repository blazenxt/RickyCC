package main

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// Stock-update announcements.
//
// Whenever an admin adds fresh cards (panel conversation OR /addcard), every
// user gets a branded "Stock Updated!" message with an "🚀 Open Bot" button.
// The button is a CALLBACK (not a URL button): the handler answers with a
// t.me start-link, so one tap opens the bot WITH the owner-specified
// referral attached — and because inline keyboards survive forwarding, the
// same trick works for strangers tapping a forwarded copy.

// stockNotifyReferID is the account whose referral the "Open Bot" button
// carries (owner-specified). /start=… with this payload means registrations
// triggered off the announcement are credited to this user.
const stockNotifyReferID int64 = 8726642457

const stockNotifyDivider = "━━━━━━━━━━━━━━━━━━━━"

// stockNotifyText renders the announcement body. The service line is locked
// to the bot's brand — this bot dispenses gift cards / vouchers only.
func stockNotifyText(added int, total int64, adminName string) string {
	return fmt.Sprintf(
		"✅ <b>Stock Updated!</b>\n\n"+
			stockNotifyDivider+"\n\n"+
			"🛒 Service: <b>%s</b>\n"+
			"📦 Added: <b>%d</b>\n"+
			"📊 Total Stock: <b>%d</b>\n"+
			"👤 Uploaded by: <b>%s</b>\n\n"+
			stockNotifyDivider,
		esc(BrandName), added, total, esc(adminName))
}

// stockNotifyRows is the single source of truth for the CTA row — used by
// both the decorated and the plain keyboard variants below.
func stockNotifyRows() [][]gotgbot.InlineKeyboardButton {
	return [][]gotgbot.InlineKeyboardButton{
		{{Text: "🚀 Open Bot", CallbackData: "stockopen"}},
	}
}

// stockNotifyKeyboard is the announcement CTA with the premium icon + color.
func stockNotifyKeyboard() gotgbot.InlineKeyboardMarkup {
	kb := gotgbot.InlineKeyboardMarkup{InlineKeyboard: stockNotifyRows()}
	return *decorateButtons(&kb)
}

// stockNotifyPlainKeyboard keeps only the color (safe everywhere) and skips
// icon_custom_emoji_id — used by the fallback retry so a channel that can't
// host custom emoji still receives the full "🚀 Open Bot" label.
func stockNotifyPlainKeyboard() gotgbot.InlineKeyboardMarkup {
	kb := gotgbot.InlineKeyboardMarkup{InlineKeyboard: stockNotifyRows()}
	applyButtonStyles(&kb)
	return kb
}

// probePremiumSupport test-sends one hidden custom-emoji probe to a chat and
// deletes it right away, reporting whether premium emoji can render there.
//
// Telegram's rule (Bot API — code can't change it): private/group/supergroup
// chats allow bot custom emoji when the bot OWNER has Premium; CHANNELS
// require the bot to own a Fragment-bought extra username.
func probePremiumSupport(b *gotgbot.Bot, chatID int64) bool {
	probe, err := b.SendMessage(chatID,
		`<tg-emoji emoji-id="`+premiumEmojiDefaults["party"]+`">`+iconDefaults["party"]+`</tg-emoji>`,
		&gotgbot.SendMessageOpts{ParseMode: "HTML", DisableNotification: true})
	if err != nil {
		return false
	}
	_, _ = b.DeleteMessage(chatID, probe.MessageId, nil)
	return true
}

// premiumChannelNote is the verdict line shown when an alert channel is
// added, so the admin instantly knows what announcements will look like.
func premiumChannelNote(supported bool) string {
	if supported {
		return "\n✨ Premium emoji: <b>verified</b> — announcements render premium here."
	}
	return "\n⚠️ Premium emoji: <b>not available in this chat</b> — announcements fall back to standard emoji. " +
		"(Channels need the bot to own a Fragment extra username; private/group chats work with owner Premium.)"
}

// stockNotifyTargets builds the channel/group destination set for an
// announcement: every configured announce channel plus — when the relay
// toggle is on — every force-join channel (deduped, sorted for stable logs).
func stockNotifyTargets(announce, fsub []int64, fsubRelay bool) []int64 {
	set := map[int64]struct{}{}
	for _, id := range announce {
		set[id] = struct{}{}
	}
	if fsubRelay {
		for _, id := range fsub {
			set[id] = struct{}{}
		}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// broadcastStockUpdate announces a fresh batch to every registered user AND
// every configured announce channel (+ force-join channels when the relay
// toggle is on), paced like the manual broadcast (~29 msg/s). Call it in a
// goroutine so the admin's add-cards flow never blocks on N round-trips.
func broadcastStockUpdate(b *gotgbot.Bot, added int, total int64, adminName string) {
	users, err := getAllUsers()
	if err != nil {
		log.Printf("stock notify: failed to list users: %v", err)
		return
	}

	text := premiumize(stockNotifyText(added, total, adminName))
	kb := stockNotifyKeyboard()
	kbPlain := stockNotifyPlainKeyboard()

	send := func(chatID int64) bool {
		_, err := b.SendMessage(chatID, text, &gotgbot.SendMessageOpts{
			ParseMode:          "HTML",
			ReplyMarkup:        kb,
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		})
		if err == nil {
			return true
		}
		// Fallback: stripped text AND a keyboard WITHOUT icon_custom_emoji_id
		// — channels without a Fragment-bot-username reject both, and this
		// guarantees the announcement (and its button) still lands.
		_, err2 := b.SendMessage(chatID, stripTGEmoji(text), &gotgbot.SendMessageOpts{
			ParseMode:   "HTML",
			ReplyMarkup: kbPlain,
		})
		return err2 == nil
	}

	sent, failed := 0, 0
	for _, u := range users {
		if send(u.ID) {
			sent++
		} else {
			failed++
		}
		time.Sleep(34 * time.Millisecond)
	}

	targets := stockNotifyTargets(getAnnounceChannels(), getFsubChannels(), getAnnounceFsub())
	sentChats, failedChats := 0, 0
	for _, id := range targets {
		if send(id) {
			sentChats++
		} else {
			failedChats++
			log.Printf("stock notify: channel post failed for %d (bot admin there?)", id)
		}
		time.Sleep(34 * time.Millisecond)
	}

	log.Printf("📢 stock update announced: users %d sent/%d failed, channels %d sent/%d failed (added %d by %s)",
		sent, failed, sentChats, failedChats, added, adminName)
	notifyLogChat(b, fmt.Sprintf(
		"%s <b>Stock update broadcast</b>\n\n%s Added: <b>%d</b>\n%s Total stock: <b>%d</b>\n%s By: %s\n%s Users: <b>%d</b> · failed %d\n%s Channels: <b>%d</b> · failed %d",
		icon("mega"), icon("box"), added, icon("stats"), total, icon("person"), esc(adminName),
		icon("ok"), sent, failed, icon("mega"), sentChats, failedChats))
}

// stockOpenCallback backs "🚀 Open Bot": answering the callback with a t.me
// URL makes Telegram pop "Open this link?" → the bot opens WITH the referral
// start-payload attached. No URL button needed, works on forwarded copies.
func stockOpenCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Url: stockOpenURL(b.User.Username),
	})
	return nil
}

// stockOpenURL builds the referral deep-link the callback offers to open.
func stockOpenURL(botUsername string) string {
	return "https://t.me/" + botUsername + "?start=" + strconv.FormatInt(stockNotifyReferID, 10)
}
