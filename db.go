package main

import (
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// User represents the structure of a user document in MongoDB
type User struct {
	ID            int64   `bson:"_id,omitempty" json:"_id,omitempty"`
	Referrer      int64   `bson:"referrer,omitempty" json:"referrer,omitempty"`
	ReferredUsers []int64 `bson:"referred_users,omitempty" json:"referred_users,omitempty"`
	AccNo         int64   `bson:"acc_no,omitempty" json:"acc_no,omitempty"`
	Balance       float64 `bson:"balance,omitempty" json:"balance,omitempty"`
	HasClaimed    bool    `bson:"has_claimed,omitempty" json:"has_claimed,omitempty"`
	ClaimedCode   string  `bson:"claimed_code,omitempty" json:"claimed_code,omitempty"`
}

// Code statuses
const (
	CodeAvailable = "available"
	CodeClaimed   = "claimed"
)

// Code represents a reward code in the stock
type Code struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Code       string             `bson:"code" json:"code"`
	Status     string             `bson:"status" json:"status"`
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
	ClaimedBy  int64              `bson:"claimed_by,omitempty" json:"claimed_by,omitempty"`
	ClaimedAt  *time.Time         `bson:"claimed_at,omitempty" json:"claimed_at,omitempty"`
}

var (
	userColl *mongo.Collection
	codeColl *mongo.Collection
)

func addUser(user User) error {
	count, err := userColl.CountDocuments(ctx, bson.M{"_id": user.ID})
	if err != nil {
		return fmt.Errorf("failed to check user existence: %v", err)
	}

	if count > 0 {
		return fmt.Errorf("user with ID %d already exists", user.ID)
	}

	_, err = userColl.InsertOne(ctx, user)
	if err != nil {
		return fmt.Errorf("failed to add user: %v", err)
	}

	return nil
}

func referUser(referrerID, newUserID int64) error {
	// Check if referrer exists
	referrer := User{}
	err := userColl.FindOne(ctx, bson.M{"_id": referrerID}).Decode(&referrer)
	if err != nil {
		return fmt.Errorf("referrer with ID %d does not exist", referrerID)
	}

	newUser := User{
		ID:       newUserID,
		Referrer: referrerID,
	}

	err = addUser(newUser)
	if err != nil {
		return err
	}

	_, err = userColl.UpdateOne(ctx, bson.M{"_id": referrerID}, bson.M{"$push": bson.M{"referred_users": newUserID}})
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

func markUserClaimed(userID int64, code string) error {
	_, err := userColl.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$set": bson.M{"has_claimed": true, "claimed_code": code}},
	)
	if err != nil {
		return fmt.Errorf("failed to mark user %d as claimed: %v", userID, err)
	}
	return nil
}

func countClaimedUsers() (int64, error) {
	return userColl.CountDocuments(ctx, bson.M{"has_claimed": true})
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

// ---------- Reward code stock ----------

// addCodes inserts new codes into the stock, skipping empty lines,
// in-batch duplicates, and codes that already exist in the database.
// Returns (added, skipped, error).
func addCodes(lines []string) (int, int, error) {
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
		return 0, 0, fmt.Errorf("no valid codes provided")
	}

	// Find which of these codes already exist in the DB
	cursor, err := codeColl.Find(ctx,
		bson.M{"code": bson.M{"$in": fresh}},
		options.Find().SetProjection(bson.M{"code": 1}),
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to check existing codes: %v", err)
	}
	var existing []Code
	if err = cursor.All(ctx, &existing); err != nil {
		return 0, 0, fmt.Errorf("failed to decode existing codes: %v", err)
	}
	dup := map[string]bool{}
	for _, c := range existing {
		dup[c.Code] = true
	}

	now := time.Now()
	var docs []interface{}
	for _, c := range fresh {
		if dup[c] {
			continue
		}
		docs = append(docs, Code{Code: c, Status: CodeAvailable, CreatedAt: now})
	}

	skipped := len(fresh) - len(docs)
	if len(docs) == 0 {
		return 0, skipped, nil
	}

	res, err := codeColl.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	if err != nil {
		return 0, skipped, fmt.Errorf("failed to insert codes: %v", err)
	}
	return len(res.InsertedIDs), skipped, nil
}

func countAvailableCodes() (int64, error) {
	return codeColl.CountDocuments(ctx, bson.M{"status": CodeAvailable})
}

func countClaimedCodes() (int64, error) {
	return codeColl.CountDocuments(ctx, bson.M{"status": CodeClaimed})
}

// claimCodeAtomic atomically marks the oldest available code as claimed by the
// user, so concurrent claims can never hand out the same code twice.
// Returns mongo.ErrNoDocuments when the stock is empty.
func claimCodeAtomic(userID int64) (*Code, error) {
	now := time.Now()
	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.After).
		SetSort(bson.M{"created_at": 1})

	var c Code
	err := codeColl.FindOneAndUpdate(ctx,
		bson.M{"status": CodeAvailable},
		bson.M{"$set": bson.M{"status": CodeClaimed, "claimed_by": userID, "claimed_at": now}},
		opts,
	).Decode(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
