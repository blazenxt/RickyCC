package main

import (
	"fmt"
	"log"
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

// stockNotifyKeyboard is the single "Open Bot" CTA (goes through the
// premium/style decorator like every other keyboard).
func stockNotifyKeyboard() gotgbot.InlineKeyboardMarkup {
	kb := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "🚀 Open Bot", CallbackData: "stockopen"}},
		},
	}
	return *decorateButtons(&kb)
}

// broadcastStockUpdate announces a fresh batch to every registered user,
// paced like the manual broadcast (~29 msg/s). Call it in a goroutine so
// the admin's add-cards flow never blocks on N network round-trips.
func broadcastStockUpdate(b *gotgbot.Bot, added int, total int64, adminName string) {
	users, err := getAllUsers()
	if err != nil {
		log.Printf("stock notify: failed to list users: %v", err)
		return
	}
	if len(users) == 0 {
		return
	}

	text := premiumize(stockNotifyText(added, total, adminName))
	kb := stockNotifyKeyboard()

	sent, failed := 0, 0
	for _, u := range users {
		_, err := b.SendMessage(u.ID, text, &gotgbot.SendMessageOpts{
			ParseMode:          "HTML",
			ReplyMarkup:        kb,
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		})
		switch {
		case err == nil:
			sent++
		default:
			// Safety net: if custom emoji ever rejects, deliver stripped.
			if _, err2 := b.SendMessage(u.ID, stripTGEmoji(text), &gotgbot.SendMessageOpts{
				ParseMode:   "HTML",
				ReplyMarkup: kb,
			}); err2 == nil {
				sent++
			} else {
				failed++
			}
		}
		time.Sleep(34 * time.Millisecond)
	}

	log.Printf("📢 stock update announced: %d sent, %d failed (added %d by %s)", sent, failed, added, adminName)
	notifyLogChat(b, fmt.Sprintf(
		"%s <b>Stock update broadcast</b>\n\n%s Added: <b>%d</b>\n%s Total stock: <b>%d</b>\n%s By: %s\n%s Delivered: <b>%d</b> · failed %d",
		icon("mega"), icon("box"), added, icon("stats"), total, icon("person"), esc(adminName), icon("ok"), sent, failed))
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
