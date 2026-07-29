package registration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/anianroid/thirdshift/internal/shared/ids"
)

var (
	ErrInvalidInvite     = errors.New("invalid invite token")
	ErrInviteUsed        = errors.New("invite token has already been used")
	ErrInviteExpired     = errors.New("invite token has expired")
	ErrInvalidBootstrap  = errors.New("invalid bootstrap token")
	ErrBootstrapUsed     = errors.New("bootstrap token has already been used")
	ErrBootstrapExpired  = errors.New("bootstrap token has expired")
	ErrRepositoryMissing = errors.New("registration repository is not configured")
)

type Repository interface {
	CreateInvite(ctx context.Context, invite InviteRecord) error
	RegisterNode(ctx context.Context, registration NodeRegistration) (RegistrationCreated, error)
	ConsumeBootstrap(ctx context.Context, nodeID, bootstrapHash string, now time.Time) error
}

type Service struct {
	Repository   Repository
	Now          func() time.Time
	InviteTTL    time.Duration
	BootstrapTTL time.Duration
	TokenBytes   int
}

type InviteRecord struct {
	ID        string
	FleetID   string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type InviteToken struct {
	ID        string
	FleetID   string
	Token     string
	ExpiresAt time.Time
}

type RegisterRequest struct {
	InviteToken             string `json:"invite_token"`
	PublicKey               string `json:"public_key"`
	HardwareFingerprintHash string `json:"hardware_fingerprint_hash"`
}

type RegisterResult struct {
	NodeID                  string    `json:"node_id"`
	BootstrapToken          string    `json:"bootstrap_token"`
	BootstrapTokenExpiresAt time.Time `json:"bootstrap_token_expires_at"`
}

type NodeRegistration struct {
	InviteTokenHash         string
	NodeID                  string
	KeyID                   string
	PublicKey               string
	HardwareFingerprintHash string
	BootstrapTokenID        string
	BootstrapTokenHash      string
	BootstrapTokenExpiresAt time.Time
	Now                     time.Time
}

type RegistrationCreated struct {
	NodeID                  string
	BootstrapTokenExpiresAt time.Time
}

func (s Service) CreateInvite(ctx context.Context, fleetID string) (InviteToken, error) {
	if s.Repository == nil {
		return InviteToken{}, ErrRepositoryMissing
	}
	if fleetID == "" {
		return InviteToken{}, fmt.Errorf("fleet id is required")
	}
	now := s.now()
	inviteID, err := ids.New("inv")
	if err != nil {
		return InviteToken{}, err
	}
	token, err := s.randomToken("tsinv")
	if err != nil {
		return InviteToken{}, err
	}
	ttl := s.InviteTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	expiresAt := now.Add(ttl).UTC()
	if err := s.Repository.CreateInvite(ctx, InviteRecord{
		ID:        inviteID,
		FleetID:   fleetID,
		TokenHash: HashToken(token),
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}); err != nil {
		return InviteToken{}, err
	}
	return InviteToken{ID: inviteID, FleetID: fleetID, Token: token, ExpiresAt: expiresAt}, nil
}

func (s Service) RegisterNode(ctx context.Context, req RegisterRequest) (RegisterResult, error) {
	if s.Repository == nil {
		return RegisterResult{}, ErrRepositoryMissing
	}
	if req.InviteToken == "" {
		return RegisterResult{}, fmt.Errorf("invite token is required")
	}
	if req.PublicKey == "" {
		return RegisterResult{}, fmt.Errorf("public key is required")
	}
	if req.HardwareFingerprintHash == "" {
		return RegisterResult{}, fmt.Errorf("hardware fingerprint hash is required")
	}
	now := s.now()
	nodeID, err := ids.New("node")
	if err != nil {
		return RegisterResult{}, err
	}
	keyID, err := ids.New("nkey")
	if err != nil {
		return RegisterResult{}, err
	}
	bootstrapID, err := ids.New("boot")
	if err != nil {
		return RegisterResult{}, err
	}
	bootstrapToken, err := s.randomToken("tsboot")
	if err != nil {
		return RegisterResult{}, err
	}
	ttl := s.BootstrapTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	expiresAt := now.Add(ttl).UTC()
	created, err := s.Repository.RegisterNode(ctx, NodeRegistration{
		InviteTokenHash:         HashToken(req.InviteToken),
		NodeID:                  nodeID,
		KeyID:                   keyID,
		PublicKey:               req.PublicKey,
		HardwareFingerprintHash: req.HardwareFingerprintHash,
		BootstrapTokenID:        bootstrapID,
		BootstrapTokenHash:      HashToken(bootstrapToken),
		BootstrapTokenExpiresAt: expiresAt,
		Now:                     now,
	})
	if err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{
		NodeID:                  created.NodeID,
		BootstrapToken:          bootstrapToken,
		BootstrapTokenExpiresAt: created.BootstrapTokenExpiresAt,
	}, nil
}

func (s Service) ConsumeBootstrap(ctx context.Context, nodeID, bootstrapToken string) error {
	if s.Repository == nil {
		return ErrRepositoryMissing
	}
	if nodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	if bootstrapToken == "" {
		return fmt.Errorf("bootstrap token is required")
	}
	return s.Repository.ConsumeBootstrap(ctx, nodeID, HashToken(bootstrapToken), s.now())
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) randomToken(prefix string) (string, error) {
	size := s.TokenBytes
	if size <= 0 {
		size = 32
	}
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate token entropy: %w", err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(data), nil
}
