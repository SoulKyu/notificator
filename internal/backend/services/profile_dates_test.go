package services

import (
	"context"
	"testing"

	authpb "notificator/internal/backend/proto/auth"
)

// TestProfileDatesAreReal guards against #177 regressing: ValidateSession and
// GetProfile must return the account's actual created_at/last_login instead
// of the webui handler inventing them with time.Now() arithmetic.
func TestProfileDatesAreReal(t *testing.T) {
	svc, sessionID := setupAuthServiceWithSession(t)
	ctx := context.Background()

	validate, err := svc.ValidateSession(ctx, &authpb.ValidateSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if validate.User.CreatedAt == nil {
		t.Fatal("ValidateSession: CreatedAt is nil, want the account's real creation time")
	}
	if validate.User.LastLogin != nil {
		t.Fatalf("ValidateSession: LastLogin = %v, want nil (never logged in)", validate.User.LastLogin)
	}

	if err := svc.db.UpdateLastLogin(validate.User.Id); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}

	profile, err := svc.GetProfile(ctx, &authpb.GetProfileRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if profile.User.CreatedAt == nil {
		t.Fatal("GetProfile: CreatedAt is nil, want the account's real creation time")
	}
	if profile.User.LastLogin == nil {
		t.Fatal("GetProfile: LastLogin is nil after UpdateLastLogin, want it populated")
	}
}
