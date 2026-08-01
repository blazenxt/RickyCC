package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
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

// fSub checks whether the user is a member of ALL configured force-join
// channels. If not, it sends one message listing join buttons for every
// missing channel plus a "Try again" button, and returns false.
func fSub(b *gotgbot.Bot, userId int64, arg string) (bool, error) {
	channels := getFsubChannels()
	if len(channels) == 0 {
		return true, nil
	}

	var buttons [][]gotgbot.InlineKeyboardButton
	for i, chatID := range channels {
		status := ""
		userMember, err := b.GetChatMember(chatID, userId, nil)
		if err != nil {
			if !isNotMemberErr(err) {
				return false, fmt.Errorf("error getting chat member: %s", err)
			}
			status = "left" // user has never joined / left the channel
		} else {
			status = userMember.MergeChatMember().Status
		}

		if !memberStatuses[status] {
			inviteLink, err := fetchInviteLink(b, chatID)
			if err != nil || inviteLink == "" {
				return false, fmt.Errorf("invite link not available for chat %d", chatID)
			}
			buttons = append(buttons, []gotgbot.InlineKeyboardButton{
				{Text: fmt.Sprintf("📢 Join Channel %d", i+1), Url: inviteLink},
			})
		}

		time.Sleep(300 * time.Millisecond)
	}

	// Member of every required channel
	if len(buttons) == 0 {
		return true, nil
	}

	tryAgainURL := fmt.Sprintf("https://t.me/%s", b.Username)
	if arg != "" {
		// Preserve the referral payload through the join flow
		tryAgainURL = fmt.Sprintf("https://t.me/%s?start=%s", b.Username, arg)
	}
	buttons = append(buttons, []gotgbot.InlineKeyboardButton{
		{Text: "✅ Joined — Try Again", Url: tryAgainURL},
	})

	_, err := b.SendMessage(userId,
		"🔒 <b>Access Locked</b>\n\n"+
			"To use this bot, join ALL of our channels first, then tap <b>Try Again</b>.",
		&gotgbot.SendMessageOpts{
			ParseMode:   "HTML",
			ReplyMarkup: &gotgbot.InlineKeyboardMarkup{InlineKeyboard: buttons},
		})

	if err != nil {
		log.Printf("Error sending message: %s", err)
	}

	return false, nil
}
