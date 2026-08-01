package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// User represents the structure of a user document in MongoDB
type User struct {
	ID            int64     `bson:"_id,omitempty" json:"_id,omitempty"`
	Name          string    `bson:"name,omitempty" json:"name,omitempty"`
	Username      string    `bson:"username,omitempty" json:"username,omitempty"`
	Referrer      int64     `bson:"referrer,omitempty" json:"referrer,omitempty"`
	ReferredUsers []int64   `bson:"referred_users,omitempty" json:"referred_users,omitempty"`
	JoinedAt      time.Time `bson:"joined_at,omitempty" json:"joined_at,omitempty"`
	Banned        bool      `bson:"banned,omitempty" json:"banned,omitempty"`
	HasClaimed    bool      `bson:"has_claimed,omitempty" json:"has_claimed,omitempty"`
	ClaimedCard   string    `bson:"claimed_code,omitempty" json:"claimed_code,omitempty"`
}

// Card statuses
const (
	CardAvailable = "available"
	CardClaimed   = "claimed"
)

// Card represents a reward card in the stock
type Card struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Card       string             `bson:"code" json:"code"`
	Status     string             `bson:"status" json:"status"`
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
	ClaimedBy  int64              `bson:"claimed_by,omitempty" json:"claimed_by,omitempty"`
	ClaimedAt  *time.Time         `bson:"claimed_at,omitempty" json:"claimed_at,omitempty"`
}

var (
	userColl *mongo.Collection
	cardColl *mongo.Collection
)

func addUser(user User) error {
	count, err := userColl.CountDocuments(ctx, bson.M{"_id": user.ID})
	if err != nil {
		return fmt.Errorf("failed to check user existence: %v", err)
	}

	if count > 0 {
		return fmt.Errorf("user with ID %d already exists", user.ID)
	}

	if user.JoinedAt.IsZero() {
		user.JoinedAt = time.Now()
	}

	_, err = userColl.InsertOne(ctx, user)
	if err != nil {
		return fmt.Errorf("failed to add user: %v", err)
	}

	return nil
}

// referUser registers newUser under referrerID and links them in the
// referrer's referred_users list.
func referUser(referrerID int64, newUser User) error {
	// Check if referrer exists
	referrer := User{}
	err := userColl.FindOne(ctx, bson.M{"_id": referrerID}).Decode(&referrer)
	if err != nil {
		return fmt.Errorf("referrer with ID %d does not exist", referrerID)
	}

	newUser.Referrer = referrerID
	err = addUser(newUser)
	if err != nil {
		return err
	}

	_, err = userColl.UpdateOne(ctx, bson.M{"_id": referrerID}, bson.M{"$push": bson.M{"referred_users": newUser.ID}})
	if err != nil {
		return fmt.Errorf("failed to update referrer's referred users: %v", err)
	}
	return nil
}

func getUser(userID int64) (*User, error) {
	user := User{}
	err := userColl.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// updateUserProfile refreshes the stored name/username (best effort).
func updateUserProfile(userID int64, name, username string) {
	_, err := userColl.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$set": bson.M{"name": name, "username": username}},
	)
	if err != nil {
		log.Printf("failed to update profile for user %d: %v", userID, err)
	}
}

func markUserClaimed(userID int64, card string) error {
	_, err := userColl.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$set": bson.M{"has_claimed": true, "claimed_code": card}},
	)
	if err != nil {
		return fmt.Errorf("failed to mark user %d as claimed: %v", userID, err)
	}
	return nil
}

func countClaimedUsers() (int64, error) {
	return userColl.CountDocuments(ctx, bson.M{"has_claimed": true})
}

func countAllUsers() (int64, error) {
	return userColl.CountDocuments(ctx, bson.M{})
}

func countBannedUsers() (int64, error) {
	return userColl.CountDocuments(ctx, bson.M{"banned": true})
}

func getAllUsers() ([]User, error) {
	cursor, err := userColl.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve users: %v", err)
	}
	defer cursor.Close(ctx)

	var users []User
	if err = cursor.All(ctx, &users); err != nil {
		return nil, fmt.Errorf("failed to decode users: %v", err)
	}
	return users, nil
}

// getRecentUsers returns the newest users (by join date), capped at limit.
func getRecentUsers(limit int64) ([]User, error) {
	opts := options.Find().SetSort(bson.M{"joined_at": -1}).SetLimit(limit)
	cursor, err := userColl.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve recent users: %v", err)
	}
	defer cursor.Close(ctx)

	var users []User
	if err = cursor.All(ctx, &users); err != nil {
		return nil, fmt.Errorf("failed to decode users: %v", err)
	}
	return users, nil
}

// ---------- Admin user actions ----------

func setUserBanned(userID int64, banned bool) error {
	res, err := userColl.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$set": bson.M{"banned": banned}},
	)
	if err != nil {
		return fmt.Errorf("failed to update ban status for user %d: %v", userID, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}
	return nil
}

// resetUserClaim clears a user's claim so they can claim a new reward.
func resetUserClaim(userID int64) error {
	res, err := userColl.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$unset": bson.M{"has_claimed": "", "claimed_code": ""}},
	)
	if err != nil {
		return fmt.Errorf("failed to reset claim for user %d: %v", userID, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}
	return nil
}

// deleteUser removes a user document and unlinks them from their referrer.
func deleteUser(userID int64) error {
	u, err := getUser(userID)
	if err != nil {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}

	if u.Referrer > 0 {
		_, err := userColl.UpdateOne(ctx,
			bson.M{"_id": u.Referrer},
			bson.M{"$pull": bson.M{"referred_users": userID}},
		)
		if err != nil {
			log.Printf("failed to unlink user %d from referrer %d: %v", userID, u.Referrer, err)
		}
	}

	res, err := userColl.DeleteOne(ctx, bson.M{"_id": userID})
	if err != nil {
		return fmt.Errorf("failed to delete user %d: %v", userID, err)
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}
	return nil
}

// ---------- Reward card stock ----------

// addCards inserts new cards into the stock, skipping empty lines,
// in-batch duplicates, and cards that already exist in the database.
// Returns (added, skipped, error).
func addCards(lines []string) (int, int, error) {
	seen := map[string]bool{}
	var fresh []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		fresh = append(fresh, l)
	}
	if len(fresh) == 0 {
		return 0, 0, fmt.Errorf("no valid cards provided")
	}

	// Find which of these cards already exist in the DB
	cursor, err := cardColl.Find(ctx,
		bson.M{"code": bson.M{"$in": fresh}},
		options.Find().SetProjection(bson.M{"code": 1}),
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to check existing cards: %v", err)
	}
	var existing []Card
	if err = cursor.All(ctx, &existing); err != nil {
		return 0, 0, fmt.Errorf("failed to decode existing codes: %v", err)
	}
	dup := map[string]bool{}
	for _, c := range existing {
		dup[c.Card] = true
	}

	now := time.Now()
	var docs []interface{}
	for _, c := range fresh {
		if dup[c] {
			continue
		}
		docs = append(docs, Card{Card: c, Status: CardAvailable, CreatedAt: now})
	}

	skipped := len(fresh) - len(docs)
	if len(docs) == 0 {
		return 0, skipped, nil
	}

	res, err := cardColl.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	if err != nil {
		return 0, skipped, fmt.Errorf("failed to insert cards: %v", err)
	}
	return len(res.InsertedIDs), skipped, nil
}

func countAvailableCards() (int64, error) {
	return cardColl.CountDocuments(ctx, bson.M{"status": CardAvailable})
}

func countClaimedCards() (int64, error) {
	return cardColl.CountDocuments(ctx, bson.M{"status": CardClaimed})
}

// claimCardAtomic atomically marks the oldest available card as claimed by the
// user, so concurrent claims can never hand out the same card twice.
// Returns mongo.ErrNoDocuments when the stock is empty.
func claimCardAtomic(userID int64) (*Card, error) {
	now := time.Now()
	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.After).
		SetSort(bson.M{"created_at": 1})

	var c Card
	err := cardColl.FindOneAndUpdate(ctx,
		bson.M{"status": CardAvailable},
		bson.M{"$set": bson.M{"status": CardClaimed, "claimed_by": userID, "claimed_at": now}},
		opts,
	).Decode(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// getRecentClaims returns the latest claimed cards (most recent first).
func getRecentClaims(limit int64) ([]Card, error) {
	opts := options.Find().SetSort(bson.M{"claimed_at": -1}).SetLimit(limit)
	cursor, err := cardColl.Find(ctx, bson.M{"status": CardClaimed}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve claims: %v", err)
	}
	defer cursor.Close(ctx)

	var codes []Card
	if err = cursor.All(ctx, &codes); err != nil {
		return nil, fmt.Errorf("failed to decode claims: %v", err)
	}
	return codes, nil
}

// clearClaimedCards permanently deletes all claimed card records.
func clearClaimedCards() (int64, error) {
	res, err := cardColl.DeleteMany(ctx, bson.M{"status": CardClaimed})
	if err != nil {
		return 0, fmt.Errorf("failed to clear claimed cards: %v", err)
	}
	return res.DeletedCount, nil
}
