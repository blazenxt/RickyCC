package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

var (
	chatInviteLinks = make(map[int64]string)
	chatCacheMutex  sync.RWMutex
	memberStatuses  = map[string]bool{
		"member":        true,
		"administrator": true,
		"creator":       true, // ChatMemberOwner status
	}
)

// isNotMemberErr reports whether a GetChatMember error simply means the user
// is not in the chat (Telegram returns "Bad Request: user not found" for users
// who have never joined), as opposed to a real API failure.
func isNotMemberErr(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "user not found") ||
		strings.Contains(s, "participant_not_a_member") ||
		strings.Contains(s, "member_not_found") ||
		strings.Contains(s, "not a member")
}

func fetchInviteLink(b *gotgbot.Bot, chatID int64) (string, error) {
	chatCacheMutex.RLock()
	if link, found := chatInviteLinks[chatID]; found {
		chatCacheMutex.RUnlock()
		return link, nil
	}
	chatCacheMutex.RUnlock()

	chatCacheMutex.Lock()
	defer chatCacheMutex.Unlock()

	if link, found := chatInviteLinks[chatID]; found {
		return link, nil
	}

	chat, err := b.GetChat(chatID, nil)
	if err != nil {
		log.Printf("Error getting chat: %s", err)
		return "", err
	}

	chatInviteLinks[chatID] = chat.InviteLink
	return chat.InviteLink, nil
}

// cacheInviteLink stores an invite link for a chat in the local cache.
func cacheInviteLink(chatID int64, link string) {
	chatCacheMutex.Lock()
	chatInviteLinks[chatID] = link
	chatCacheMutex.Unlock()
}

// fsubRetryData packs the (optional) /start referral argument into the
// callback payload of the "Joined — Try Again" button. callback_data is
// limited to 64 bytes, so over-long junk args are truncated — referrals are
// numeric IDs and never get close.
func fsubRetryData(arg string) string {
	if len(arg) > 48 {
		arg = arg[:48]
	}
	if arg == "" {
		return "fsj"
	}
	return "fsj." + arg
}

// parseFsubRetryData recovers the referral argument from the callback
// payload — "" when there was none.
func parseFsubRetryData(data string) string {
	data = strings.TrimPrefix(data, "fsj")
	return strings.TrimPrefix(data, ".")
}

func lockFsubText() string {
	return icon("lock")+" <b>Access Locked</b>\n\n" +
		"To use this bot, join ALL of our channels first, then tap <b>Joined — Try Again</b>."
}

// fsubMissingButtons returns one join button (with a working invite link)
// per channel the user is NOT a member of; empty means fully joined.
func fsubMissingButtons(b *gotgbot.Bot, userId int64) ([][]gotgbot.InlineKeyboardButton, error) {
	var buttons [][]gotgbot.InlineKeyboardButton
	for i, chatID := range getFsubChannels() {
		status := ""
		userMember, err := b.GetChatMember(chatID, userId, nil)
		if err != nil {
			if !isNotMemberErr(err) {
				return nil, fmt.Errorf("error getting chat member: %s", err)
			}
			status = "left" // user has never joined / left the channel
		} else {
			status = userMember.MergeChatMember().Status
		}

		if !memberStatuses[status] {
			inviteLink, err := fetchInviteLink(b, chatID)
			if err != nil || inviteLink == "" {
				return nil, fmt.Errorf("invite link not available for chat %d", chatID)
			}
			buttons = append(buttons, []gotgbot.InlineKeyboardButton{
				{Text: fmt.Sprintf("📢 Join Channel %d", i+1), Url: inviteLink},
			})
		}

		time.Sleep(300 * time.Millisecond)
	}
	return buttons, nil
}

// fSub checks whether the user is a member of ALL configured force-join
// channels. If not, it sends one message listing join buttons for every
// missing channel plus a "Try again" button, and returns false.
//
// The retry button is a CALLBACK ("fsj.<ref>"), not a t.me deep-link: users
// already inside the bot chat tapping a same-chat t.me/<bot>?start=… link
// often only get the profile card instead of a fresh /start — the callback
// re-checks membership in place and continues the flow reliably. The
// referral argument rides inside the callback data, so it survives the
// whole join dance exactly like before.
func fSub(b *gotgbot.Bot, userId int64, arg string) (bool, error) {
	if len(getFsubChannels()) == 0 {
		return true, nil
	}

	buttons, err := fsubMissingButtons(b, userId)
	if err != nil {
		return false, err
	}

	// Member of every required channel
	if len(buttons) == 0 {
		return true, nil
	}

	buttons = append(buttons, []gotgbot.InlineKeyboardButton{
		{Text: "✅ Joined — Try Again", CallbackData: fsubRetryData(arg)},
	})

	_, err = b.SendMessage(userId, lockFsubText(), &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: premiumizeButtons(&gotgbot.InlineKeyboardMarkup{InlineKeyboard: buttons}),
	})
	if err != nil {
		log.Printf("Error sending message: %s", err)
	}

	return false, nil
}

// fsubRetryCallback backs "✅ Joined — Try Again": re-checks membership and
// either refreshes the lock message in place (still missing channels) or
// continues the normal /start flow with the original referral argument.
func fsubRetryCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	arg := parseFsubRetryData(query.Data)

	buttons, err := fsubMissingButtons(b, user.Id)
	if err != nil {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "⚠️ Something went wrong — please send /start again.",
			ShowAlert: true,
		})
		return fmt.Errorf("fsub retry: %v", err)
	}

	if len(buttons) > 0 {
		// Still not a member everywhere — keep ONE lock message, refreshed.
		buttons = append(buttons, []gotgbot.InlineKeyboardButton{
			{Text: "✅ Joined — Try Again", CallbackData: fsubRetryData(arg)},
		})
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ You haven't joined ALL channels yet — join every one, then tap again!",
			ShowAlert: true,
		})
		// "Message is not modified" errors are expected and harmless here.
		_, _, _ = msg.EditText(b, lockFsubText(), &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: *premiumizeButtons(&gotgbot.InlineKeyboardMarkup{InlineKeyboard: buttons}),
		})
		return nil
	}

	_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "✅ Channels verified!"})

	// Recycle the lock message into a status line, then continue exactly as
	// /start would (banned check → home screen, or captcha for new users).
	_, _, _ = msg.EditText(b,
		icon("ok")+" <b>All channels verified!</b> Setting things up…",
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	return continueAfterFsub(b, ctx, arg)
}
