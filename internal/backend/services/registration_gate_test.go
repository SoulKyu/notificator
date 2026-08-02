package services

import (
	"context"
	"testing"

	"notificator/config"
	"notificator/internal/backend/database"
	authpb "notificator/internal/backend/proto/auth"
)

func newTestDB(t *testing.T) *database.GormDB {
	t.Helper()

	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestRegisterRejectedWhenOAuthOnly guards against the fail-open bug in #64:
// on an OAuth-only deployment, Register must reject regardless of what the
// caller (WebUI or a direct gRPC client) knows about the OAuth config.
func TestRegisterRejectedWhenOAuthOnly(t *testing.T) {
	db := newTestDB(t)
	oauthService := &OAuthService{config: &config.OAuthPortalConfig{Enabled: true, DisableClassicAuth: true}}
	svc := NewAuthServiceGorm(db, oauthService, nil, true)

	resp, err := svc.Register(context.Background(), &authpb.RegisterRequest{Username: "attacker", Email: "a@example.com", Password: "password"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Fatal("expected registration to be rejected on an OAuth-only deployment")
	}
	if _, err := db.GetUserByUsername("attacker"); err == nil {
		t.Fatal("expected no user to be created")
	}
}

// TestLoginRejectedWhenOAuthOnly covers the identical fail-open shape in Login.
func TestLoginRejectedWhenOAuthOnly(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.CreateUser("alice", "alice@example.com", "hash"); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	oauthService := &OAuthService{config: &config.OAuthPortalConfig{Enabled: true, DisableClassicAuth: true}}
	svc := NewAuthServiceGorm(db, oauthService, nil, true)

	resp, err := svc.Login(context.Background(), &authpb.LoginRequest{Username: "alice", Password: "hash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Fatal("expected password login to be rejected on an OAuth-only deployment")
	}
}

// TestRegisterRejectedWhenAllowRegistrationDisabled covers the new
// backend.allow_registration flag on deployments that don't use OAuth at all.
func TestRegisterRejectedWhenAllowRegistrationDisabled(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuthServiceGorm(db, nil, nil, false)

	resp, err := svc.Register(context.Background(), &authpb.RegisterRequest{Username: "bob", Email: "bob@example.com", Password: "password"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Fatal("expected registration to be rejected when backend.allow_registration=false")
	}
	if resp.Message == "" {
		t.Fatal("expected a clear error message")
	}
	if _, err := db.GetUserByUsername("bob"); err == nil {
		t.Fatal("expected no user to be created")
	}
}

// TestRegisterAllowedByDefault is the control: existing deployments keep working.
func TestRegisterAllowedByDefault(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuthServiceGorm(db, nil, nil, true)

	resp, err := svc.Register(context.Background(), &authpb.RegisterRequest{Username: "carol", Email: "carol@example.com", Password: "password"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected registration to succeed, got message: %q", resp.Message)
	}
}
