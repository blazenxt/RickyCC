package main

import (
	"log"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// Group policy — the bot is a PRIVATE-CHAT-ONLY experience.
//
// guardBotChatMember receives my_chat_member updates: the moment the bot is
// added to (or promoted in) any group/supergroup it says a one-line goodbye
// and leaves. The configured LOG chat is the single exemption — database
// backups/restores live there.
//
// Channels are untouched: force-join channels NEED the bot as admin (invite
// links + chat_join_request updates), and the log destination may itself be
// a channel.
func guardBotChatMember(b *gotgbot.Bot, ctx *ext.Context) error {
	upd := ctx.Update.MyChatMember
	if upd == nil {
		return nil
	}
	if upd.Chat.Type != "group" && upd.Chat.Type != "supergroup" {
		return nil
	}
	status := upd.NewChatMember.MergeChatMember().Status
	if status != "member" && status != "administrator" {
		return nil // being removed/restricted — nothing to do
	}
	if logID := getLogChat(); logID != 0 && upd.Chat.Id == logID {
		return nil // log/backup group is allowed
	}

	log.Printf("👋 bot added to group %d (%q) — leaving, private chats only", upd.Chat.Id, upd.Chat.Title)
	_, _ = b.SendMessage(upd.Chat.Id,
		"⚠️ I work only in private chats.\n\nStart me directly: @"+b.User.Username, nil)
	_, err := b.LeaveChat(upd.Chat.Id, nil)
	if err != nil {
		log.Printf("failed to leave group %d: %v", upd.Chat.Id, err)
	}
	return nil
}
