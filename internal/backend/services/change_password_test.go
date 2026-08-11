package services

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"notificator/config"
	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
	authpb "notificator/internal/backend/proto/auth"
)

func setupAuthServiceWithPassword(t *testing.T, password string) (*AuthServiceGorm, *database.GormDB, string, string) {
	t.Helper()

	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	user, err := db.CreateUser("alice", "alice@example.com", string(hash))
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionID := "test-session"
	if err := db.CreateSession(user.ID, sessionID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	return NewAuthServiceGorm(db, nil, nil, true), db, user.ID, sessionID
}

func TestChangePassword(t *testing.T) {
	svc, db, userID, sessionID := setupAuthServiceWithPassword(t, "old-password")
	ctx := context.Background()

	otherSessionID := "other-session"
	if err := db.CreateSession(userID, otherSessionID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create other session: %v", err)
	}

	if resp, err := svc.ChangePassword(ctx, &authpb.ChangePasswordRequest{
		SessionId:   sessionID,
		OldPassword: "wrong-password",
		NewPassword: "new-password",
	}); err != nil || resp.Success {
		t.Fatalf("wrong old password: Success = %v, err = %v, want success=false", resp.Success, err)
	}

	if resp, err := svc.ChangePassword(ctx, &authpb.ChangePasswordRequest{
		SessionId:   sessionID,
		OldPassword: "old-password",
		NewPassword: "new-password",
	}); err != nil || !resp.Success {
		t.Fatalf("correct old password: Success = %v (error %q), err = %v, want success=true", resp.Success, resp.Error, err)
	}

	// New password now authenticates, old one doesn't.
	if resp, err := svc.Login(ctx, &authpb.LoginRequest{Username: "alice", Password: "old-password"}); err != nil || resp.Success {
		t.Fatalf("old password should no longer work: Success = %v, err = %v", resp.Success, err)
	}
	if resp, err := svc.Login(ctx, &authpb.LoginRequest{Username: "alice", Password: "new-password"}); err != nil || !resp.Success {
		t.Fatalf("new password should work: Success = %v (message %q), err = %v", resp.Success, resp.Message, err)
	}

	// The current session must survive the password change...
	if _, err := db.GetUserBySession(sessionID); err != nil {
		t.Fatalf("expected current session %q to remain valid after password change: %v", sessionID, err)
	}
	// ...but other sessions must be invalidated.
	if _, err := db.GetUserBySession(otherSessionID); err == nil {
		t.Fatalf("expected other session %q to be invalidated after password change", otherSessionID)
	}
}

func TestChangePasswordRejectsOAuthUser(t *testing.T) {
	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	user, err := db.CreateOAuthUser("github", "12345", &models.OAuthUserInfo{
		Email:    "bob@example.com",
		Username: "bob",
	})
	if err != nil {
		t.Fatalf("failed to create OAuth user: %v", err)
	}
	sessionID := "oauth-session"
	if err := db.CreateSession(user.ID, sessionID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	svc := NewAuthServiceGorm(db, nil, nil, true)
	resp, err := svc.ChangePassword(context.Background(), &authpb.ChangePasswordRequest{
		SessionId:   sessionID,
		OldPassword: "whatever",
		NewPassword: "new-password",
	})
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected OAuth-backed account to be rejected, got success")
	}
}
