package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var errUnknown = errors.New("account or session not found")
var errEmailTaken = errors.New("account email already exists")

type Account struct {
	ID    string     `bson:"_id" json:"id"`
	Email string     `bson:"email" json:"email"`
	Role  authn.Role `bson:"role" json:"role"`
}

func (a Account) Actor() authn.Actor { return authn.Actor{ID: a.ID, Role: a.Role} }

type credential struct {
	Account      `bson:",inline"`
	PasswordHash []byte `bson:"password_hash"`
}

type session struct {
	Hash      string    `bson:"_id"`
	Account   Account   `bson:"account"`
	ExpiresAt time.Time `bson:"expires_at"`
}

type Store struct {
	users    *mongo.Collection
	sessions *mongo.Collection
}

func NewStore(database *mongo.Database) *Store {
	return &Store{users: database.Collection("accounts"), sessions: database.Collection("sessions")}
}

func (s *Store) EnsureIndexes(ctx context.Context) error {
	if _, err := s.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true),
	}); err != nil {
		return fmt.Errorf("prepare accounts: %w", err)
	}
	_, err := s.sessions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0),
	})
	return err
}

func (s *Store) hasOperator(ctx context.Context) (bool, error) {
	err := s.users.FindOne(ctx, bson.M{"role": authn.Operator}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) bootstrap(ctx context.Context, item credential) error {
	// The reserved ID makes initialization atomic across gateway replicas.
	item.ID = "bootstrap-operator"
	_, err := s.users.UpdateOne(ctx, bson.M{"_id": item.ID},
		bson.M{"$setOnInsert": item}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Store) create(ctx context.Context, item credential) error {
	_, err := s.users.InsertOne(ctx, item)
	if mongo.IsDuplicateKeyError(err) {
		return errEmailTaken
	}
	return err
}

func (s *Store) credential(ctx context.Context, email string) (credential, error) {
	var result credential
	err := s.users.FindOne(ctx, bson.M{"email": email}).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return credential{}, errUnknown
	}
	return result, err
}

func (s *Store) createSession(ctx context.Context, item session) error {
	_, err := s.sessions.InsertOne(ctx, item)
	return err
}

func (s *Store) activeSession(ctx context.Context, hash string, now time.Time) (Account, error) {
	var item session
	err := s.sessions.FindOne(ctx, bson.M{"_id": hash, "expires_at": bson.M{"$gt": now}}).Decode(&item)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Account{}, errUnknown
	}
	return item.Account, err
}

func (s *Store) revoke(ctx context.Context, hash string) error {
	_, err := s.sessions.DeleteOne(ctx, bson.M{"_id": hash})
	return err
}
