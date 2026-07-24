package services

import (
	"context"
	"testing"
	"time"

	"notificator/config"
	"notificator/internal/backend/database"
	authpb "notificator/internal/backend/proto/auth"
)

func setupAuthServiceWithSession(t *testing.T) (*AuthServiceGorm, string) {
	t.Helper()

	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	user, err := db.CreateUser("alice", "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionID := "test-session"
	if err := db.CreateSession(user.ID, sessionID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	return NewAuthServiceGorm(db, nil), sessionID
}

func TestUpdateTimezone(t *testing.T) {
	svc, sessionID := setupAuthServiceWithSession(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		req         *authpb.UpdateTimezoneRequest
		wantSuccess bool
	}{
		{"missing session", &authpb.UpdateTimezoneRequest{Timezone: "UTC"}, false},
		{"invalid session", &authpb.UpdateTimezoneRequest{SessionId: "nope", Timezone: "UTC"}, false},
		{"invalid timezone", &authpb.UpdateTimezoneRequest{SessionId: sessionID, Timezone: "Mars/Olympus"}, false},
		{"valid", &authpb.UpdateTimezoneRequest{SessionId: sessionID, Timezone: "UTC"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.UpdateTimezone(ctx, tc.req)
			if err != nil {
				t.Fatalf("UpdateTimezone: %v", err)
			}
			if resp.Success != tc.wantSuccess {
				t.Fatalf("Success = %v (error %q), want %v", resp.Success, resp.Error, tc.wantSuccess)
			}
		})
	}

	// The rejected Mars/Olympus must not have been persisted; the valid UTC must be.
	profile, err := svc.GetProfile(ctx, &authpb.GetProfileRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if profile.User.Timezone != "UTC" {
		t.Fatalf("GetProfile timezone = %q, want %q", profile.User.Timezone, "UTC")
	}

	validate, err := svc.ValidateSession(ctx, &authpb.ValidateSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if validate.User.Timezone != "UTC" {
		t.Fatalf("ValidateSession timezone = %q, want %q", validate.User.Timezone, "UTC")
	}
}
