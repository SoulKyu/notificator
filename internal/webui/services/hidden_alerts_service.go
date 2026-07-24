package services

import (
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"

	"notificator/internal/backend/models"
	alertpb "notificator/internal/backend/proto/alert"
	"notificator/internal/webui/client"
	webuimodels "notificator/internal/webui/models"
)

// sessionIdleTTL matches the backend session TTL: cache entries for sessions
// idle longer than this are unreachable (session expired) and can be dropped.
const sessionIdleTTL = 7 * 24 * time.Hour

// HiddenAlertsService manages hidden alerts and rules for users
type HiddenAlertsService struct {
	backendClient       *client.BackendClient
	mu                  sync.RWMutex
	userHiddenAlerts    map[string]map[string]bool             // userID -> fingerprint -> hidden
	userHiddenRules     map[string][]models.UserHiddenRule     // userID -> rules
	compiledRegexRules  map[string]map[string]*regexp.Regexp   // userID -> ruleID -> compiled regex
	lastAccess          map[string]time.Time                   // userID -> last LoadUserData call
}

// NewHiddenAlertsService creates a new hidden alerts service
func NewHiddenAlertsService(backendClient *client.BackendClient) *HiddenAlertsService {
	service := &HiddenAlertsService{
		backendClient:      backendClient,
		userHiddenAlerts:   make(map[string]map[string]bool),
		userHiddenRules:    make(map[string][]models.UserHiddenRule),
		compiledRegexRules: make(map[string]map[string]*regexp.Regexp),
		lastAccess:         make(map[string]time.Time),
	}
	
	// Load initial data
	service.LoadAllUserData()
	
	return service
}

// LoadAllUserData loads all hidden alerts and rules for all users
func (s *HiddenAlertsService) LoadAllUserData() {
	// This would typically be called on startup or periodically
	// For now, we'll load data on-demand per user
	log.Println("HiddenAlertsService initialized")
}

// LoadUserData loads hidden alerts and rules for a specific user using sessionID
func (s *HiddenAlertsService) LoadUserData(sessionID string) error {
	// Get userID from session for cache key
	// Note: We'll need to pass userID separately or get it from session
	// For now, we'll use sessionID as the cache key

	// C4 fix: perform gRPC calls BEFORE acquiring the write lock to avoid
	// holding the lock across potentially long-running I/O operations.
	// GetUserHiddenAlerts and GetUserHiddenRules do not take s.mu, so this is safe.

	// Fetch hidden alerts from backend (no lock held)
	hiddenAlerts, hiddenAlertsErr := s.GetUserHiddenAlerts(sessionID)
	if hiddenAlertsErr != nil {
		log.Printf("Failed to load hidden alerts: %v", hiddenAlertsErr)
	}

	// Fetch hidden rules from backend (no lock held)
	rules, hiddenRulesErr := s.GetUserHiddenRules(sessionID)
	if hiddenRulesErr != nil {
		log.Printf("Failed to load hidden rules: %v", hiddenRulesErr)
	}

	// Pre-compile regex patterns outside the lock
	compiledRegexes := make(map[string]*regexp.Regexp)
	if hiddenRulesErr == nil {
		for _, rule := range rules {
			if rule.IsRegex && rule.LabelValue != "" {
				regex, err := regexp.Compile(rule.LabelValue)
				if err != nil {
					log.Printf("Failed to compile regex for rule %s: %v", rule.ID, err)
				} else {
					compiledRegexes[rule.ID] = regex
				}
			}
		}
	}

	// Acquire write lock only to write results into the maps
	s.mu.Lock()
	defer s.mu.Unlock()

	// Replace cached hidden alerts wholesale so entries removed in the backend
	// (or stale regexes for deleted rules) do not accumulate.
	if hiddenAlertsErr == nil {
		freshAlerts := make(map[string]bool, len(hiddenAlerts))
		for _, alert := range hiddenAlerts {
			freshAlerts[alert.Fingerprint] = true
		}
		s.userHiddenAlerts[sessionID] = freshAlerts
	} else if s.userHiddenAlerts[sessionID] == nil {
		s.userHiddenAlerts[sessionID] = make(map[string]bool)
	}

	if hiddenRulesErr == nil {
		s.userHiddenRules[sessionID] = rules
		s.compiledRegexRules[sessionID] = compiledRegexes
	} else {
		if s.userHiddenRules[sessionID] == nil {
			s.userHiddenRules[sessionID] = []models.UserHiddenRule{}
		}
		if s.compiledRegexRules[sessionID] == nil {
			s.compiledRegexRules[sessionID] = make(map[string]*regexp.Regexp)
		}
	}

	s.lastAccess[sessionID] = time.Now()
	// ponytail: opportunistic sweep instead of a janitor goroutine — sessions
	// idle past the backend session TTL are dropped on the next load by anyone.
	s.sweepIdleSessionsLocked()

	return nil
}

// sweepIdleSessionsLocked drops cache entries for sessions with no LoadUserData
// call within sessionIdleTTL. Caller must hold s.mu.
func (s *HiddenAlertsService) sweepIdleSessionsLocked() {
	for sessionID, last := range s.lastAccess {
		if time.Since(last) >= sessionIdleTTL {
			delete(s.userHiddenAlerts, sessionID)
			delete(s.userHiddenRules, sessionID)
			delete(s.compiledRegexRules, sessionID)
			delete(s.lastAccess, sessionID)
		}
	}
}

// IsAlertHidden checks if an alert is hidden for a user using sessionID
func (s *HiddenAlertsService) IsAlertHidden(sessionID string, alert *webuimodels.DashboardAlert) bool {
	// C3 fix: check the map under a read lock to avoid a racy map read.
	s.mu.RLock()
	loaded := s.userHiddenAlerts[sessionID] != nil
	s.mu.RUnlock()

	if !loaded {
		// Try to load data if not cached; LoadUserData acquires its own write lock.
		_ = s.LoadUserData(sessionID)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	
	// Check specific hidden alerts
	if s.userHiddenAlerts[sessionID] != nil {
		if s.userHiddenAlerts[sessionID][alert.Fingerprint] {
			return true
		}
	}
	
	// Check hidden rules
	rules := s.userHiddenRules[sessionID]
	for _, rule := range rules {
		if !rule.IsEnabled {
			continue
		}
		
		// Check if the alert has the label
		labelValue, exists := alert.Labels[rule.LabelKey]
		if !exists {
			continue
		}
		
		// Check if the label value matches
		if rule.IsRegex {
			// Use compiled regex
			if regex, ok := s.compiledRegexRules[sessionID][rule.ID]; ok {
				if regex.MatchString(labelValue) {
					return true
				}
			}
		} else {
			// Exact match or empty value (match all)
			if rule.LabelValue == "" || rule.LabelValue == labelValue {
				return true
			}
		}
	}
	
	return false
}

// IsAlertHiddenByFilter checks if an alert is hidden by filter-specific hidden alerts/rules
// This is used for per-filter hiding that's additive to global hidden alerts
func (s *HiddenAlertsService) IsAlertHiddenByFilter(
	alert *webuimodels.DashboardAlert,
	filterHiddenAlerts []webuimodels.FilterHiddenAlert,
	filterHiddenRules []webuimodels.FilterHiddenRule,
	compiledRules map[int]*regexp.Regexp,
) bool {
	if alert == nil {
		return false
	}

	// Check specific hidden alerts by fingerprint
	for _, hiddenAlert := range filterHiddenAlerts {
		if hiddenAlert.Fingerprint == alert.Fingerprint {
			return true
		}
	}

	// Check hidden rules
	for i, rule := range filterHiddenRules {
		if !rule.IsEnabled {
			continue
		}

		// Check if the alert has the label
		labelValue, exists := alert.Labels[rule.LabelKey]
		if !exists {
			continue
		}

		// Check if the label value matches
		if rule.IsRegex {
			// Use compiled regex if available
			if compiledRules != nil {
				if regex, ok := compiledRules[i]; ok && regex != nil {
					if regex.MatchString(labelValue) {
						return true
					}
				}
			}
		} else {
			// Exact match or empty value (match all)
			if rule.LabelValue == "" || rule.LabelValue == labelValue {
				return true
			}
		}
	}

	return false
}

// CompileFilterRules pre-compiles regex rules for a filter preset
// Returns a map from rule index to compiled regex
func (s *HiddenAlertsService) CompileFilterRules(rules []webuimodels.FilterHiddenRule) map[int]*regexp.Regexp {
	compiledRules := make(map[int]*regexp.Regexp)

	for i, rule := range rules {
		if rule.IsRegex && rule.LabelValue != "" {
			regex, err := regexp.Compile(rule.LabelValue)
			if err != nil {
				log.Printf("Failed to compile filter regex for rule %s: %v", rule.Name, err)
				continue
			}
			compiledRules[i] = regex
		}
	}

	return compiledRules
}

// HideAlert hides a specific alert for a user
func (s *HiddenAlertsService) HideAlert(sessionID string, alert *webuimodels.DashboardAlert, reason string, impersonateUserID ...string) error {
	err := s.backendClient.HideAlert(sessionID, alert.Fingerprint, alert.AlertName, alert.Instance, reason, impersonateUserID...)
	if err != nil {
		return fmt.Errorf("failed to hide alert in backend: %w", err)
	}
	
	// Update the cache
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userHiddenAlerts[sessionID] == nil {
		s.userHiddenAlerts[sessionID] = make(map[string]bool)
	}
	s.userHiddenAlerts[sessionID][alert.Fingerprint] = true
	
	return nil
}

// UnhideAlert unhides a specific alert for a user
func (s *HiddenAlertsService) UnhideAlert(sessionID, fingerprint string, impersonateUserID ...string) error {
	err := s.backendClient.UnhideAlert(sessionID, fingerprint, impersonateUserID...)
	if err != nil {
		return fmt.Errorf("failed to unhide alert in backend: %w", err)
	}
	
	// Update the cache
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userHiddenAlerts[sessionID] != nil {
		delete(s.userHiddenAlerts[sessionID], fingerprint)
	}
	
	return nil
}

// GetUserHiddenAlerts gets all hidden alerts for a user
func (s *HiddenAlertsService) GetUserHiddenAlerts(sessionID string, impersonateUserID ...string) ([]models.UserHiddenAlert, error) {
	pbHiddenAlerts, err := s.backendClient.GetUserHiddenAlerts(sessionID, impersonateUserID...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch hidden alerts from backend: %w", err)
	}
	
	// Convert protobuf models to regular models
	var hiddenAlerts []models.UserHiddenAlert
	for _, pbAlert := range pbHiddenAlerts {
		hiddenAlerts = append(hiddenAlerts, models.UserHiddenAlert{
			ID:          pbAlert.Id,
			UserID:      pbAlert.UserId,
			Fingerprint: pbAlert.Fingerprint,
			AlertName:   pbAlert.AlertName,
			Instance:    pbAlert.Instance,
			Reason:      pbAlert.Reason,
			CreatedAt:   pbAlert.CreatedAt.AsTime(),
			UpdatedAt:   pbAlert.UpdatedAt.AsTime(),
		})
	}
	
	return hiddenAlerts, nil
}

// SaveHiddenRule saves or updates a hidden rule for a user
func (s *HiddenAlertsService) SaveHiddenRule(sessionID string, rule *models.UserHiddenRule, impersonateUserID ...string) error {
	// Validate regex if needed
	if rule.IsRegex {
		_, err := regexp.Compile(rule.LabelValue)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	// Convert to protobuf model
	pbRule := &alertpb.UserHiddenRule{
		Id:          rule.ID,
		UserId:      rule.UserID,
		Name:        rule.Name,
		Description: rule.Description,
		LabelKey:    rule.LabelKey,
		LabelValue:  rule.LabelValue,
		IsRegex:     rule.IsRegex,
		IsEnabled:   rule.IsEnabled,
		Priority:    int32(rule.Priority),
	}

	_, err := s.backendClient.SaveHiddenRule(sessionID, pbRule, impersonateUserID...)
	if err != nil {
		return fmt.Errorf("failed to save hidden rule in backend: %w", err)
	}
	
	// Invalidate the cache to force reload
	s.InvalidateCache(sessionID)
	
	return nil
}

// RemoveHiddenRule removes a hidden rule for a user
func (s *HiddenAlertsService) RemoveHiddenRule(sessionID, ruleID string, impersonateUserID ...string) error {
	err := s.backendClient.RemoveHiddenRule(sessionID, ruleID, impersonateUserID...)
	if err != nil {
		return fmt.Errorf("failed to remove hidden rule in backend: %w", err)
	}
	
	// Invalidate the cache to force reload
	s.InvalidateCache(sessionID)
	
	return nil
}

// InvalidateCache clears the cache for a specific session
func (s *HiddenAlertsService) InvalidateCache(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	delete(s.userHiddenAlerts, sessionID)
	delete(s.userHiddenRules, sessionID)
	delete(s.compiledRegexRules, sessionID)
	delete(s.lastAccess, sessionID)
}

// GetUserHiddenRules gets all hidden rules for a user
func (s *HiddenAlertsService) GetUserHiddenRules(sessionID string, impersonateUserID ...string) ([]models.UserHiddenRule, error) {
	pbRules, err := s.backendClient.GetUserHiddenRules(sessionID, impersonateUserID...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch hidden rules from backend: %w", err)
	}
	
	// Convert protobuf models to regular models
	var rules []models.UserHiddenRule
	for _, pbRule := range pbRules {
		rules = append(rules, models.UserHiddenRule{
			ID:          pbRule.Id,
			UserID:      pbRule.UserId,
			Name:        pbRule.Name,
			Description: pbRule.Description,
			LabelKey:    pbRule.LabelKey,
			LabelValue:  pbRule.LabelValue,
			IsRegex:     pbRule.IsRegex,
			IsEnabled:   pbRule.IsEnabled,
			Priority:    int(pbRule.Priority),
			CreatedAt:   pbRule.CreatedAt.AsTime(),
			UpdatedAt:   pbRule.UpdatedAt.AsTime(),
		})
	}
	
	return rules, nil
}

// ClearAllHiddenAlerts removes all hidden alerts for a user
func (s *HiddenAlertsService) ClearAllHiddenAlerts(sessionID string, impersonateUserID ...string) error {
	err := s.backendClient.ClearAllHiddenAlerts(sessionID, impersonateUserID...)
	if err != nil {
		return fmt.Errorf("failed to clear hidden alerts in backend: %w", err)
	}
	
	// Clear the cache
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userHiddenAlerts[sessionID] != nil {
		s.userHiddenAlerts[sessionID] = make(map[string]bool)
	}
	
	return nil
}

// FilterHiddenAlerts filters out hidden alerts from a list
func (s *HiddenAlertsService) FilterHiddenAlerts(sessionID string, alerts []*webuimodels.DashboardAlert, includeHidden bool) []*webuimodels.DashboardAlert {
	// Ensure user data is loaded
	s.LoadUserData(sessionID)
	
	if includeHidden {
		// Return only hidden alerts
		var hiddenAlerts []*webuimodels.DashboardAlert
		for _, alert := range alerts {
			if s.IsAlertHidden(sessionID, alert) {
				alert.IsHidden = true
				alert.HiddenBy = sessionID
				hiddenAlerts = append(hiddenAlerts, alert)
			}
		}
		return hiddenAlerts
	} else {
		// Return only non-hidden alerts
		var visibleAlerts []*webuimodels.DashboardAlert
		for _, alert := range alerts {
			if !s.IsAlertHidden(sessionID, alert) {
				visibleAlerts = append(visibleAlerts, alert)
			}
		}
		return visibleAlerts
	}
}

// GetHiddenAlertsCount returns the count of hidden alerts for a user
func (s *HiddenAlertsService) GetHiddenAlertsCount(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	if s.userHiddenAlerts[userID] != nil {
		return len(s.userHiddenAlerts[userID])
	}
	return 0
}