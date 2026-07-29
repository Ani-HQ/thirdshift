package registration

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInviteLifecycleSingleUse(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	service := Service{
		Repository:   repo,
		Now:          func() time.Time { return now },
		InviteTTL:    time.Hour,
		BootstrapTTL: time.Minute,
		TokenBytes:   8,
	}
	invite, err := service.CreateInvite(context.Background(), "fleet_01J0M000000000000000000000")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	req := RegisterRequest{
		InviteToken:             invite.Token,
		PublicKey:               "test-public-key",
		HardwareFingerprintHash: "sha256:hardware",
	}
	first, err := service.RegisterNode(context.Background(), req)
	if err != nil {
		t.Fatalf("register first node: %v", err)
	}
	if first.NodeID == "" || first.BootstrapToken == "" {
		t.Fatalf("registration result missing ids: %#v", first)
	}
	if _, err := service.RegisterNode(context.Background(), req); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("second register err = %v, want ErrInviteUsed", err)
	}
}

func TestInviteLifecycleExpiry(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	service := Service{
		Repository: repo,
		Now:        func() time.Time { return now },
		InviteTTL:  time.Second,
		TokenBytes: 8,
	}
	invite, err := service.CreateInvite(context.Background(), "fleet_01J0M000000000000000000000")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	now = now.Add(2 * time.Second)
	_, err = service.RegisterNode(context.Background(), RegisterRequest{
		InviteToken:             invite.Token,
		PublicKey:               "test-public-key",
		HardwareFingerprintHash: "sha256:hardware",
	})
	if !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired invite err = %v, want ErrInviteExpired", err)
	}
}

func TestBootstrapLifecycleSingleUseAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	service := Service{
		Repository:   repo,
		Now:          func() time.Time { return now },
		InviteTTL:    time.Hour,
		BootstrapTTL: time.Second,
		TokenBytes:   8,
	}
	invite, err := service.CreateInvite(context.Background(), "fleet_01J0M000000000000000000000")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	registered, err := service.RegisterNode(context.Background(), RegisterRequest{
		InviteToken:             invite.Token,
		PublicKey:               "test-public-key",
		HardwareFingerprintHash: "sha256:hardware",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := service.ConsumeBootstrap(context.Background(), registered.NodeID, registered.BootstrapToken); err != nil {
		t.Fatalf("first bootstrap consume: %v", err)
	}
	if err := service.ConsumeBootstrap(context.Background(), registered.NodeID, registered.BootstrapToken); !errors.Is(err, ErrBootstrapUsed) {
		t.Fatalf("second bootstrap err = %v, want ErrBootstrapUsed", err)
	}

	invite2, err := service.CreateInvite(context.Background(), "fleet_01J0M000000000000000000000")
	if err != nil {
		t.Fatalf("create second invite: %v", err)
	}
	registered2, err := service.RegisterNode(context.Background(), RegisterRequest{
		InviteToken:             invite2.Token,
		PublicKey:               "test-public-key-2",
		HardwareFingerprintHash: "sha256:hardware-2",
	})
	if err != nil {
		t.Fatalf("register second: %v", err)
	}
	now = now.Add(2 * time.Second)
	if err := service.ConsumeBootstrap(context.Background(), registered2.NodeID, registered2.BootstrapToken); !errors.Is(err, ErrBootstrapExpired) {
		t.Fatalf("expired bootstrap err = %v, want ErrBootstrapExpired", err)
	}
}

type memoryRepo struct {
	invites    map[string]memoryInvite
	bootstraps map[string]memoryBootstrap
}

type memoryInvite struct {
	record InviteRecord
	status string
}

type memoryBootstrap struct {
	nodeID    string
	hash      string
	expiresAt time.Time
	used      bool
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		invites:    map[string]memoryInvite{},
		bootstraps: map[string]memoryBootstrap{},
	}
}

func (m *memoryRepo) CreateInvite(_ context.Context, invite InviteRecord) error {
	m.invites[invite.TokenHash] = memoryInvite{record: invite, status: "active"}
	return nil
}

func (m *memoryRepo) RegisterNode(_ context.Context, registration NodeRegistration) (RegistrationCreated, error) {
	invite, ok := m.invites[registration.InviteTokenHash]
	if !ok {
		return RegistrationCreated{}, ErrInvalidInvite
	}
	if invite.status == "used" {
		return RegistrationCreated{}, ErrInviteUsed
	}
	if !registration.Now.Before(invite.record.ExpiresAt) {
		invite.status = "expired"
		m.invites[registration.InviteTokenHash] = invite
		return RegistrationCreated{}, ErrInviteExpired
	}
	invite.status = "used"
	m.invites[registration.InviteTokenHash] = invite
	m.bootstraps[registration.NodeID+"|"+registration.BootstrapTokenHash] = memoryBootstrap{
		nodeID:    registration.NodeID,
		hash:      registration.BootstrapTokenHash,
		expiresAt: registration.BootstrapTokenExpiresAt,
	}
	return RegistrationCreated{NodeID: registration.NodeID, BootstrapTokenExpiresAt: registration.BootstrapTokenExpiresAt}, nil
}

func (m *memoryRepo) ConsumeBootstrap(_ context.Context, nodeID, bootstrapHash string, now time.Time) error {
	key := nodeID + "|" + bootstrapHash
	bootstrap, ok := m.bootstraps[key]
	if !ok {
		return ErrInvalidBootstrap
	}
	if bootstrap.used {
		return ErrBootstrapUsed
	}
	if !now.Before(bootstrap.expiresAt) {
		return ErrBootstrapExpired
	}
	bootstrap.used = true
	m.bootstraps[key] = bootstrap
	return nil
}
