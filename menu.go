package main

import (
	"log"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Bot "/" menu commands.
//
// Published at boot via setMyCommands — Telegram persists them server-side,
// so every boot just refreshes the same sets:
//   - all private chats → the public four,
//   - all group chats  → EMPTY (the bot refuses to work in groups — menu
//     hidden entirely),
//   - owner + every admin's own chat → public four + the admin toolset
//     (scoped per chat, so regular users never see admin commands).

func userMenuCommands() []gotgbot.BotCommand {
	return []gotgbot.BotCommand{
		{Command: "start", Description: "Start the bot / refresh home"},
		{Command: "help", Description: "How the bot works"},
		{Command: "progress", Description: "Your referrals & rewards"},
		{Command: "info", Description: "Your account info"},
	}
}

func adminMenuCommands() []gotgbot.BotCommand {
	cmds := append([]gotgbot.BotCommand{}, userMenuCommands()...)
	return append(cmds,
		gotgbot.BotCommand{Command: "admin", Description: "Open the admin panel"},
		gotgbot.BotCommand{Command: "addcard", Description: "Add cards to stock"},
		gotgbot.BotCommand{Command: "stock", Description: "View card stock"},
		gotgbot.BotCommand{Command: "stats", Description: "Bot statistics"},
		gotgbot.BotCommand{Command: "broadcast", Description: "Broadcast a message (reply)"},
		gotgbot.BotCommand{Command: "backupdb", Description: "Download database backup"},
		gotgbot.BotCommand{Command: "userbot", Description: "Premium channel editor login (owner)"},
	)
}

// menuAdminChatIDs = owner + configured admins (deduped) — everyone who
// should see the extended menu.
func menuAdminChatIDs() []int64 {
	seen := map[int64]struct{}{}
	var out []int64
	add := func(id int64) {
		if id == 0 {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(OwnerID)
	for _, id := range getAdminIDs() {
		add(id)
	}
	return out
}

// setupBotMenu pushes all command sets to Telegram. Failures are logged but
// never fatal — the bot works the same with or without a menu.
func setupBotMenu(b *gotgbot.Bot) {
	if _, err := b.SetMyCommands(userMenuCommands(), &gotgbot.SetMyCommandsOpts{
		Scope: gotgbot.BotCommandScopeAllPrivateChats{},
	}); err != nil {
		log.Printf("setMyCommands (private chats): %v", err)
	}

	if _, err := b.SetMyCommands([]gotgbot.BotCommand{}, &gotgbot.SetMyCommandsOpts{
		Scope: gotgbot.BotCommandScopeAllGroupChats{},
	}); err != nil {
		log.Printf("setMyCommands (group chats): %v", err)
	}

	adminCmds := adminMenuCommands()
	for _, id := range menuAdminChatIDs() {
		if _, err := b.SetMyCommands(adminCmds, &gotgbot.SetMyCommandsOpts{
			Scope: gotgbot.BotCommandScopeChat{ChatId: id},
		}); err != nil {
			log.Printf("setMyCommands (admin %d): %v", id, err)
		}
	}
}
