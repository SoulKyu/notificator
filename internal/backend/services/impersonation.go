package services

import (
	"fmt"
	"log"

	"notificator/config"
	"notificator/internal/backend/models"
)

// resolveTargetUser returns the effective user ID for a request, enforcing the
// backend impersonation allowlist. impersonateUserID equal to the session
// user's own ID is always allowed. Any other value requires the session user
// to be on adminConfig's allowlist, checked by username or email.
func resolveTargetUser(adminConfig *config.AdminConfig, user *models.User, impersonateUserID, rpcName string) (string, error) {
	if impersonateUserID == "" || impersonateUserID == user.ID {
		return user.ID, nil
	}

	if adminConfig == nil || (!adminConfig.CanImpersonate(user.Username) && !adminConfig.CanImpersonate(user.Email)) {
		log.Printf("[IMPERSONATION] denied: requester=%s (%s) target=%s rpc=%s", user.ID, user.Username, impersonateUserID, rpcName)
		return "", fmt.Errorf("not authorized to impersonate user %s", impersonateUserID)
	}

	return impersonateUserID, nil
}
