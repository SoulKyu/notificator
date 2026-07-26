package services

import (
	"context"
	"testing"
	"time"

	"gorm.io/datatypes"

	"notificator/config"
	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
	mainmodels "notificator/internal/models"

	alertpb "notificator/internal/backend/proto/alert"
)

func setupImpersonationTest(t *testing.T, allowedUsers []string) (*AlertServiceGorm, *database.GormDB, *models.User, *models.User) {
	t.Helper()

	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
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

	svc := NewAlertServiceGorm(db, &config.AdminConfig{ImpersonationAllowedUsers: allowedUsers})

	return svc, db, victim, requester
}

func seedVictimColorPreference(t *testing.T, db *database.GormDB, victimID string) {
	t.Helper()

	err := db.SaveUserColorPreferences(victimID, []mainmodels.UserColorPreference{
		{
			ID:              "pref-1",
			Color:           "red",
			ColorType:       "custom",
			LabelConditions: datatypes.JSON([]byte(`{}`)),
		},
	})
	if err != nil {
		t.Fatalf("failed to seed victim color preference: %v", err)
	}
}

func TestGetUserColorPreferences_OrdinaryUserCannotImpersonate(t *testing.T) {
	svc, db, victim, requester := setupImpersonationTest(t, nil)
	seedVictimColorPreference(t, db, victim.ID)

	resp, err := svc.GetUserColorPreferences(context.Background(), &alertpb.GetUserColorPreferencesRequest{
		SessionId:         "requester-session",
		ImpersonateUserId: victim.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected impersonation to be denied for non-admin %s", requester.Username)
	}
	if len(resp.Preferences) != 0 {
		t.Fatalf("expected no preferences leaked to unauthorized requester")
	}
}

func TestGetUserColorPreferences_AllowlistedAdminCanImpersonate(t *testing.T) {
	svc, db, victim, _ := setupImpersonationTest(t, []string{"requester"})
	seedVictimColorPreference(t, db, victim.ID)

	resp, err := svc.GetUserColorPreferences(context.Background(), &alertpb.GetUserColorPreferencesRequest{
		SessionId:         "requester-session",
		ImpersonateUserId: victim.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected allowlisted admin to impersonate successfully, got message: %s", resp.Message)
	}
	if len(resp.Preferences) != 1 {
		t.Fatalf("expected 1 preference, got %d", len(resp.Preferences))
	}
}

func TestSaveUserColorPreferences_OrdinaryUserCannotImpersonate(t *testing.T) {
	svc, db, victim, requester := setupImpersonationTest(t, nil)
	seedVictimColorPreference(t, db, victim.ID)

	resp, err := svc.SaveUserColorPreferences(context.Background(), &alertpb.SaveUserColorPreferencesRequest{
		SessionId:         "requester-session",
		ImpersonateUserId: victim.ID,
		Preferences:       nil, // empty list: would wipe the victim's preferences if honored
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected impersonation to be denied for non-admin %s", requester.Username)
	}

	prefs, err := db.GetUserColorPreferences(victim.ID)
	if err != nil {
		t.Fatalf("failed to read back victim preferences: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("expected victim's preference to survive denied impersonation, got %d", len(prefs))
	}
}

func TestSaveUserColorPreferences_AllowlistedAdminCanImpersonate(t *testing.T) {
	svc, db, victim, _ := setupImpersonationTest(t, []string{"requester"})
	seedVictimColorPreference(t, db, victim.ID)

	resp, err := svc.SaveUserColorPreferences(context.Background(), &alertpb.SaveUserColorPreferencesRequest{
		SessionId:         "requester-session",
		ImpersonateUserId: victim.ID,
		Preferences:       nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected allowlisted admin to impersonate successfully, got message: %s", resp.Message)
	}

	prefs, err := db.GetUserColorPreferences(victim.ID)
	if err != nil {
		t.Fatalf("failed to read back victim preferences: %v", err)
	}
	if len(prefs) != 0 {
		t.Fatalf("expected allowlisted admin's write to take effect, got %d preferences left", len(prefs))
	}
}

func TestSaveUserColorPreferences_SelfImpersonationAlwaysAllowed(t *testing.T) {
	svc, db, _, requester := setupImpersonationTest(t, nil)

	resp, err := svc.SaveUserColorPreferences(context.Background(), &alertpb.SaveUserColorPreferencesRequest{
		SessionId:         "requester-session",
		ImpersonateUserId: requester.ID,
		Preferences: []*alertpb.UserColorPreference{
			{Id: "own-pref", Color: "blue", ColorType: "custom"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected impersonate_user_id equal to own session user to be allowed, got message: %s", resp.Message)
	}

	prefs, err := db.GetUserColorPreferences(requester.ID)
	if err != nil {
		t.Fatalf("failed to read back requester preferences: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("expected 1 preference for requester, got %d", len(prefs))
	}
}
