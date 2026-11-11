package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

const passwordCost = 12
const sessionTTL = 12 * time.Hour

var errBusy = apperrors.New(http.StatusTooManyRequests, "AUTH_BUSY", "Too many authentication requests. Try again shortly")

type Service struct {
	store        *Store
	dummyHash    []byte
	hashSlots    chan struct{}
	passwordCost int
}

func NewService(store *Store) (*Service, error) {
	return newService(store, passwordCost)
}

func newService(store *Store, cost int) (*Service, error) {
	dummyHash, err := bcrypt.GenerateFromPassword([]byte(rand.Text()), cost)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, dummyHash: dummyHash, hashSlots: make(chan struct{}, 4), passwordCost: cost}, nil
}

func (s *Service) Bootstrap(ctx context.Context, email, password string) error {
	exists, err := s.store.hasOperator(ctx)
	if err != nil || exists {
		return err
	}
	if email == "" || password == "" {
		return fmt.Errorf("set MOBIX_BOOTSTRAP_EMAIL and MOBIX_BOOTSTRAP_PASSWORD for the first operator")
	}
	item, err := s.newCredential(email, password, authn.Operator)
	if err != nil {
		return fmt.Errorf("invalid bootstrap operator: %w", err)
	}
	if err := s.store.bootstrap(ctx, item); err != nil {
		return fmt.Errorf("initialize operator: %w", err)
	}
	return nil
}

func (s *Service) Create(ctx context.Context, email, password string, role authn.Role) (Account, error) {
	if authn.From(ctx).Role != authn.Operator {
		return Account{}, apperrors.ErrForbidden
	}
	if role != authn.Rider && role != authn.Driver && role != authn.Operator {
		return Account{}, apperrors.ErrInvalidInput
	}
	if !s.takeHashSlot() {
		return Account{}, errBusy
	}
	defer s.releaseHashSlot()
	item, err := s.newCredential(email, password, role)
	if err != nil {
		return Account{}, err
	}
	if err := s.store.create(ctx, item); err != nil {
		if errors.Is(err, errEmailTaken) {
			return Account{}, apperrors.ErrAlreadyExists
		}
		return Account{}, fmt.Errorf("create account: %w", err)
	}
	return item.Account, nil
}

type LoginResult struct {
	Token     string    `json:"access_token"`
	TokenType string    `json:"token_type"`
	ExpiresAt time.Time `json:"expires_at"`
	Account   Account   `json:"account"`
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	email = normalizeEmail(email)
	if len(email) > 254 || len(password) == 0 || len(password) > 72 {
		return LoginResult{}, apperrors.ErrUnauthorized
	}
	if !s.takeHashSlot() {
		return LoginResult{}, errBusy
	}
	defer s.releaseHashSlot()

	item, err := s.store.credential(ctx, email)
	if err != nil && !errors.Is(err, errUnknown) {
		return LoginResult{}, fmt.Errorf("read account credentials: %w", err)
	}
	hash := item.PasswordHash
	if errors.Is(err, errUnknown) {
		hash = s.dummyHash
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || err != nil {
		return LoginResult{}, apperrors.ErrUnauthorized
	}
	token := base64.RawURLEncoding.EncodeToString(randBytes())
	expires := time.Now().UTC().Add(sessionTTL).Truncate(time.Millisecond)
	if err := s.store.createSession(ctx, session{Hash: tokenHash(token), Account: item.Account, ExpiresAt: expires}); err != nil {
		return LoginResult{}, fmt.Errorf("store session: %w", err)
	}
	return LoginResult{Token: token, TokenType: "Bearer", ExpiresAt: expires, Account: item.Account}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Account, error) {
	if len(token) != base64.RawURLEncoding.EncodedLen(32) {
		return Account{}, apperrors.ErrUnauthorized
	}
	account, err := s.store.activeSession(ctx, tokenHash(token), time.Now())
	if errors.Is(err, errUnknown) {
		return Account{}, apperrors.ErrUnauthorized
	}
	if err != nil {
		return Account{}, fmt.Errorf("read session: %w", err)
	}
	return account, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.revoke(ctx, tokenHash(token))
}

func (s *Service) takeHashSlot() bool {
	select {
	case s.hashSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) releaseHashSlot() { <-s.hashSlots }

func (s *Service) newCredential(email, password string, role authn.Role) (credential, error) {
	email = normalizeEmail(email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || len(password) < 12 || len(password) > 72 {
		return credential{}, apperrors.New(http.StatusBadRequest, "INVALID_CREDENTIALS", "Use a valid email and a password of 12 to 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.passwordCost)
	if err != nil {
		return credential{}, err
	}
	return credential{Account: Account{ID: bson.NewObjectID().Hex(), Email: email, Role: role}, PasswordHash: hash}, nil
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func randBytes() []byte {
	var bytes [32]byte
	rand.Read(bytes[:])
	return bytes[:]
}

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
