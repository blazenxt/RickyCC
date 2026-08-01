package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var settingsColl *mongo.Collection

// Runtime bot configuration. Env vars only seed these on first boot —
// afterwards everything is edited from the admin panel and persisted in
// the "settings" collection.
var (
	// ReferralTarget is how many friends unlock a reward (plain var; admin-only writes).
	ReferralTarget = 5
	// ClaimsPaused blocks reward claims while the owner restocks, etc.
	ClaimsPaused = false

	cfgMu        sync.RWMutex
	logChatID    int64
	fsubChannels []int64
)

// botSettings is the persisted configuration document (_id: "config").
type botSettings struct {
	ID             string  `bson:"_id"`
	LogChatID      int64   `bson:"log_chat_id"`
	FsubChannels   []int64 `bson:"fsub_channels"`
	ReferralTarget int     `bson:"referral_target"`
	ClaimsPaused   bool    `bson:"claims_paused"`
}

// loadConfig loads settings from MongoDB, seeding them from env/defaults on
// first boot, and applies them to the runtime globals.
func loadConfig(envLogChatID int64, envFsubIDs []int64) {
	s := botSettings{}
	err := settingsColl.FindOne(ctx, bson.M{"_id": "config"}).Decode(&s)
	if err != nil {
		// First boot — seed from env / defaults
		s = botSettings{
			ID:             "config",
			LogChatID:      envLogChatID,
			FsubChannels:   envFsubIDs,
			ReferralTarget: ReferralTarget,
			ClaimsPaused:   false,
		}
		if _, err := settingsColl.InsertOne(ctx, s); err != nil {
			log.Printf("config: failed to seed settings: %v", err)
		}
		log.Printf("config: seeded from env (log=%d, fsub=%v, target=%d)",
			s.LogChatID, s.FsubChannels, s.ReferralTarget)
	}

	applyConfig(s)
	log.Printf("config loaded: log=%d fsub=%v target=%d claimsPaused=%v",
		s.LogChatID, s.FsubChannels, ReferralTarget, ClaimsPaused)
}

func applyConfig(s botSettings) {
	cfgMu.Lock()
	logChatID = s.LogChatID
	fsubChannels = append([]int64(nil), s.FsubChannels...)
	cfgMu.Unlock()

	if s.ReferralTarget > 0 {
		ReferralTarget = s.ReferralTarget
	}
	ClaimsPaused = s.ClaimsPaused
}

// persistSetting $sets the given fields on the config document (upsert).
func persistSetting(fields bson.M) error {
	_, err := settingsColl.UpdateOne(ctx,
		bson.M{"_id": "config"},
		bson.M{"$set": fields},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to save settings: %v", err)
	}
	return nil
}

// ---------- Getters / setters ----------

func getLogChat() int64 {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return logChatID
}

func setLogChat(id int64) error {
	if err := persistSetting(bson.M{"log_chat_id": id}); err != nil {
		return err
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

// addFsubChannel appends a channel to the force-join list.
// Returns false if it was already present.
func addFsubChannel(id int64) (bool, error) {
	cur := getFsubChannels()
	for _, c := range cur {
		if c == id {
			return false, nil
		}
	}
	next := append(cur, id)
	if err := persistSetting(bson.M{"fsub_channels": next}); err != nil {
		return false, err
	}
	cfgMu.Lock()
	fsubChannels = next
	cfgMu.Unlock()
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
	if err := persistSetting(bson.M{"fsub_channels": next}); err != nil {
		return false, err
	}
	cfgMu.Lock()
	fsubChannels = next
	cfgMu.Unlock()
	return true, nil
}

func clearFsubChannels() error {
	empty := []int64{}
	if err := persistSetting(bson.M{"fsub_channels": empty}); err != nil {
		return err
	}
	cfgMu.Lock()
	fsubChannels = empty
	cfgMu.Unlock()
	return nil
}

func setReferralTarget(n int) error {
	if n < 1 {
		return fmt.Errorf("target must be at least 1")
	}
	if err := persistSetting(bson.M{"referral_target": n}); err != nil {
		return err
	}
	ReferralTarget = n
	return nil
}

func setClaimsPaused(paused bool) error {
	if err := persistSetting(bson.M{"claims_paused": paused}); err != nil {
		return err
	}
	ClaimsPaused = paused
	return nil
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
	if _, err := b.SendMessage(id, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"}); err != nil {
		log.Printf("failed to send to log chat %d: %v", id, err)
	}
}
