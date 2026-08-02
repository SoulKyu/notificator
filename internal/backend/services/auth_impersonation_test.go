package services

import (
	"context"
	"testing"
	"time"

	"notificator/config"
	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
	authpb "notificator/internal/backend/proto/auth"
)

func setupAuthServiceImpersonationTest(t *testing.T, allowedUsers []string) (*AuthServiceGorm, *database.GormDB, *models.User, *models.User) {
	t.Helper()

	t.Setenv(database.EncryptionKeyEnvVar, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	victim, err := db.CreateUser("victim", "victim@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to create victim: %v", err)
	}
	requester, err := db.CreateUser("requester", "requester@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to create requester: %v", err)
	}

	if err := db.CreateSession(victim.ID, "victim-session", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create victim session: %v", err)
	}
	if err := db.CreateSession(requester.ID, "requester-session", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create requester session: %v", err)
	}

	svc := NewAuthServiceGorm(db, nil, &config.AdminConfig{ImpersonationAllowedUsers: allowedUsers}, true)

	return svc, db, victim, requester
}

// GetUserGroups

func TestGetUserGroups_MissingSessionRejected(t *testing.T) {
	svc, db, victim, _ := setupAuthServiceImpersonationTest(t, nil)
	if err := db.SyncUserGroups(victim.ID, "github", []models.OAuthGroupInfo{{ID: "g1", Name: "victim-group", Type: "team"}}); err != nil {
		t.Fatalf("failed to seed victim group: %v", err)
	}

	resp, err := svc.GetUserGroups(context.Background(), &authpb.GetUserGroupsRequest{UserId: victim.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 0 {
		t.Fatalf("expected no groups without a session, got %d", len(resp.Groups))
	}
}

func TestGetUserGroups_OrdinaryUserCannotImpersonate(t *testing.T) {
	svc, db, victim, _ := setupAuthServiceImpersonationTest(t, nil)
	if err := db.SyncUserGroups(victim.ID, "github", []models.OAuthGroupInfo{{ID: "g1", Name: "victim-group", Type: "team"}}); err != nil {
		t.Fatalf("failed to seed victim group: %v", err)
	}

	resp, err := svc.GetUserGroups(context.Background(), &authpb.GetUserGroupsRequest{
		SessionId: "requester-session",
		UserId:    victim.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 0 {
		t.Fatalf("expected victim's groups not to leak to unauthorized requester, got %d", len(resp.Groups))
	}
}

func TestGetUserGroups_AllowlistedAdminCanImpersonate(t *testing.T) {
	svc, db, victim, _ := setupAuthServiceImpersonationTest(t, []string{"requester"})
	if err := db.SyncUserGroups(victim.ID, "github", []models.OAuthGroupInfo{{ID: "g1", Name: "victim-group", Type: "team"}}); err != nil {
		t.Fatalf("failed to seed victim group: %v", err)
	}

	resp, err := svc.GetUserGroups(context.Background(), &authpb.GetUserGroupsRequest{
		SessionId: "requester-session",
		UserId:    victim.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 1 {
		t.Fatalf("expected allowlisted admin to read victim's groups, got %d", len(resp.Groups))
	}
}

func TestGetUserGroups_SelfAccessAllowed(t *testing.T) {
	svc, db, _, requester := setupAuthServiceImpersonationTest(t, nil)
	if err := db.SyncUserGroups(requester.ID, "github", []models.OAuthGroupInfo{{ID: "g1", Name: "own-group", Type: "team"}}); err != nil {
		t.Fatalf("failed to seed requester group: %v", err)
	}

	resp, err := svc.GetUserGroups(context.Background(), &authpb.GetUserGroupsRequest{
		SessionId: "requester-session",
		UserId:    requester.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Groups) != 1 {
		t.Fatalf("expected requester to read their own groups, got %d", len(resp.Groups))
	}
}

// GetUserSentryConfig

func TestGetUserSentryConfig_MissingSessionRejected(t *testing.T) {
	svc, db, victim, _ := setupAuthServiceImpersonationTest(t, nil)
	if err := db.SaveUserSentryConfig(victim.ID, "victim-token", "https://sentry.io"); err != nil {
		t.Fatalf("failed to seed victim sentry config: %v", err)
	}

	resp, err := svc.GetUserSentryConfig(context.Background(), &authpb.GetUserSentryConfigRequest{UserId: victim.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success || resp.Config != nil {
		t.Fatalf("expected no config without a session, got success=%v config=%v", resp.Success, resp.Config)
	}
}

func TestGetUserSentryConfig_OrdinaryUserCannotImpersonate(t *testing.T) {
	svc, db, victim, _ := setupAuthServiceImpersonationTest(t, nil)
	if err := db.SaveUserSentryConfig(victim.ID, "victim-token", "https://sentry.io"); err != nil {
		t.Fatalf("failed to seed victim sentry config: %v", err)
	}

	resp, err := svc.GetUserSentryConfig(context.Background(), &authpb.GetUserSentryConfigRequest{
		SessionId: "requester-session",
		UserId:    victim.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success || resp.Config != nil {
		t.Fatalf("expected victim's Sentry config not to leak to unauthorized requester, got success=%v config=%v", resp.Success, resp.Config)
	}
}

func TestGetUserSentryConfig_AllowlistedAdminCanImpersonate(t *testing.T) {
	svc, db, victim, _ := setupAuthServiceImpersonationTest(t, []string{"requester"})
	if err := db.SaveUserSentryConfig(victim.ID, "victim-token", "https://sentry.io"); err != nil {
		t.Fatalf("failed to seed victim sentry config: %v", err)
	}

	resp, err := svc.GetUserSentryConfig(context.Background(), &authpb.GetUserSentryConfigRequest{
		SessionId: "requester-session",
		UserId:    victim.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success || resp.Config == nil {
		t.Fatalf("expected allowlisted admin to read victim's Sentry config, got success=%v config=%v", resp.Success, resp.Config)
	}
}

func TestGetUserSentryConfig_SelfAccessAllowed(t *testing.T) {
	svc, db, _, requester := setupAuthServiceImpersonationTest(t, nil)
	if err := db.SaveUserSentryConfig(requester.ID, "own-token", "https://sentry.io"); err != nil {
		t.Fatalf("failed to seed requester sentry config: %v", err)
	}

	resp, err := svc.GetUserSentryConfig(context.Background(), &authpb.GetUserSentryConfigRequest{
		SessionId: "requester-session",
		UserId:    requester.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success || resp.Config == nil {
		t.Fatalf("expected requester to read their own Sentry config, got success=%v config=%v", resp.Success, resp.Config)
	}
}

// SyncUserGroups: the OAuth service is nil in tests, so a request that clears
// the authorization gate always fails afterward with "OAuth service not
// configured" instead of the impersonation-denial error. That distinguishes
// "authorization passed" from "authorization was denied" without needing a
// live OAuth provider.

func TestSyncUserGroups_MissingSessionRejected(t *testing.T) {
	svc, _, victim, _ := setupAuthServiceImpersonationTest(t, nil)

	resp, err := svc.SyncUserGroups(context.Background(), &authpb.SyncUserGroupsRequest{
		UserId:   victim.ID,
		Provider: "github",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success || resp.Error != "Session ID is required" {
		t.Fatalf("expected sync without a session to be rejected, got success=%v error=%q", resp.Success, resp.Error)
	}
}

func TestSyncUserGroups_OrdinaryUserCannotImpersonate(t *testing.T) {
	svc, _, victim, _ := setupAuthServiceImpersonationTest(t, nil)

	resp, err := svc.SyncUserGroups(context.Background(), &authpb.SyncUserGroupsRequest{
		SessionId: "requester-session",
		UserId:    victim.ID,
		Provider:  "github",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success || resp.Error != "Not authorized to impersonate this user" {
		t.Fatalf("expected sync of another user's groups to be denied, got success=%v error=%q", resp.Success, resp.Error)
	}
}

func TestSyncUserGroups_AllowlistedAdminPassesAuthorization(t *testing.T) {
	svc, _, victim, _ := setupAuthServiceImpersonationTest(t, []string{"requester"})

	resp, err := svc.SyncUserGroups(context.Background(), &authpb.SyncUserGroupsRequest{
		SessionId: "requester-session",
		UserId:    victim.ID,
		Provider:  "github",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success || resp.Error != "OAuth service not configured" {
		t.Fatalf("expected allowlisted admin to clear authorization and fail downstream, got success=%v error=%q", resp.Success, resp.Error)
	}
}

func TestSyncUserGroups_SelfAccessPassesAuthorization(t *testing.T) {
	svc, _, _, requester := setupAuthServiceImpersonationTest(t, nil)

	resp, err := svc.SyncUserGroups(context.Background(), &authpb.SyncUserGroupsRequest{
		SessionId: "requester-session",
		UserId:    requester.ID,
		Provider:  "github",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success || resp.Error != "OAuth service not configured" {
		t.Fatalf("expected self-sync to clear authorization and fail downstream, got success=%v error=%q", resp.Success, resp.Error)
	}
}
