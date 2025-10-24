package services

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
)

// RuleEngine handles parsing and applying on-call rules to filter statistics
type RuleEngine struct {
	db *database.GormDB
}

// NewRuleEngine creates a new rule engine instance
func NewRuleEngine(db *database.GormDB) *RuleEngine {
	return &RuleEngine{
		db: db,
	}
}

// ==================== Rule Validation ====================

// ValidateRule checks if a rule is valid before saving
func (re *RuleEngine) ValidateRule(config *models.RuleConfig) error {
	if config == nil {
		return fmt.Errorf("rule config cannot be nil")
	}

	// Validate logic operator
	if config.Logic != "AND" && config.Logic != "OR" {
		return fmt.Errorf("logic must be 'AND' or 'OR', got: %s", config.Logic)
	}

	// Must have at least one criterion
	if len(config.Criteria) == 0 {
		return fmt.Errorf("rule must have at least one criterion")
	}

	// Validate each criterion
	for i, criterion := range config.Criteria {
		if err := re.validateCriterion(&criterion); err != nil {
			return fmt.Errorf("criterion %d is invalid: %w", i, err)
		}
	}

	return nil
}

// validateCriterion validates a single criterion
func (re *RuleEngine) validateCriterion(criterion *models.RuleCriterion) error {
	// Validate type
	validTypes := []string{"severity", "label", "alert_name"}
	if !contains(validTypes, criterion.Type) {
		return fmt.Errorf("invalid criterion type: %s (must be one of: %v)", criterion.Type, validTypes)
	}

	// Validate operator based on type
	switch criterion.Type {
	case "severity":
		return re.validateSeverityCriterion(criterion)
	case "label":
		return re.validateLabelCriterion(criterion)
	case "alert_name":
		return re.validateAlertNameCriterion(criterion)
	}

	return nil
}

func (re *RuleEngine) validateSeverityCriterion(criterion *models.RuleCriterion) error {
	validOperators := []string{"in", "equals", "not_equals"}
	if !contains(validOperators, criterion.Operator) {
		return fmt.Errorf("invalid operator for severity: %s (must be one of: %v)", criterion.Operator, validOperators)
	}

	// Validate values
	if criterion.Operator == "in" {
		if len(criterion.Values) == 0 {
			return fmt.Errorf("'in' operator requires at least one value")
		}
		// Validate severity values
		validSeverities := []string{"critical", "warning", "info"}
		for _, sev := range criterion.Values {
			if !contains(validSeverities, strings.ToLower(sev)) {
				return fmt.Errorf("invalid severity: %s (must be one of: %v)", sev, validSeverities)
			}
		}
	} else {
		if criterion.Value == "" {
			return fmt.Errorf("operator '%s' requires a value", criterion.Operator)
		}
		validSeverities := []string{"critical", "warning", "info"}
		if !contains(validSeverities, strings.ToLower(criterion.Value)) {
			return fmt.Errorf("invalid severity: %s (must be one of: %v)", criterion.Value, validSeverities)
		}
	}

	return nil
}

func (re *RuleEngine) validateLabelCriterion(criterion *models.RuleCriterion) error {
	validOperators := []string{"equals", "not_equals", "contains", "regex"}
	if !contains(validOperators, criterion.Operator) {
		return fmt.Errorf("invalid operator for label: %s (must be one of: %v)", criterion.Operator, validOperators)
	}

	// Label key is required
	if criterion.Key == "" {
		return fmt.Errorf("label criterion requires a key")
	}

	// Value is required
	if criterion.Value == "" {
		return fmt.Errorf("label criterion requires a value")
	}

	// Validate regex if operator is "regex"
	if criterion.Operator == "regex" {
		if _, err := regexp.Compile(criterion.Value); err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	return nil
}

func (re *RuleEngine) validateAlertNameCriterion(criterion *models.RuleCriterion) error {
	validOperators := []string{"equals", "contains", "regex", "starts_with", "ends_with"}
	if !contains(validOperators, criterion.Operator) {
		return fmt.Errorf("invalid operator for alert_name: %s (must be one of: %v)", criterion.Operator, validOperators)
	}

	// Pattern is required
	if criterion.Pattern == "" && criterion.Value == "" {
		return fmt.Errorf("alert_name criterion requires a pattern or value")
	}

	// Use pattern for regex, value for others
	patternToCheck := criterion.Pattern
	if patternToCheck == "" {
		patternToCheck = criterion.Value
	}

	// Validate regex if operator is "regex"
	if criterion.Operator == "regex" {
		if _, err := regexp.Compile(patternToCheck); err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	return nil
}

// ==================== Rule Application ====================

// ApplyRulesToQuery applies user's on-call rules to a GORM query
// Returns modified query with WHERE clauses based on rule criteria
func (re *RuleEngine) ApplyRulesToQuery(userID string, baseQuery *gorm.DB) (*gorm.DB, error) {
	// Load user's active rules
	rules, err := re.db.GetActiveOnCallRules(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user rules: %w", err)
	}

	// No rules = no filtering (return all statistics)
	if len(rules) == 0 {
		return baseQuery, nil
	}

	// For simplicity, use the first active rule
	// TODO: Support multiple rules with priority/combination logic
	rule := rules[0]

	// Parse rule config
	config, err := database.ParseRuleConfig(rule.RuleConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rule config: %w", err)
	}

	// Apply criteria to query
	query := baseQuery

	// Build WHERE clause based on logic operator
	if config.Logic == "OR" {
		// For OR logic, build a single WHERE clause with OR conditions
		query = re.applyOrLogic(query, config.Criteria)
	} else {
		// For AND logic, apply each criterion sequentially
		for _, criterion := range config.Criteria {
			query = re.applyCriterion(query, &criterion)
		}
	}

	return query, nil
}

// applyCriterion applies a single criterion to the query
func (re *RuleEngine) applyCriterion(query *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	switch criterion.Type {
	case "severity":
		return re.applySeverityCriterion(query, criterion)
	case "label":
		return re.applyLabelCriterion(query, criterion)
	case "alert_name":
		return re.applyAlertNameCriterion(query, criterion)
	default:
		// Unknown type, return query unchanged
		return query
	}
}

// applySeverityCriterion applies severity filtering
func (re *RuleEngine) applySeverityCriterion(query *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	switch criterion.Operator {
	case "in":
		// Normalize severities to lowercase
		normalized := make([]string, len(criterion.Values))
		for i, v := range criterion.Values {
			normalized[i] = strings.ToLower(v)
		}
		return query.Where("LOWER(severity) IN ?", normalized)
	case "equals":
		return query.Where("LOWER(severity) = ?", strings.ToLower(criterion.Value))
	case "not_equals":
		return query.Where("LOWER(severity) != ?", strings.ToLower(criterion.Value))
	default:
		return query
	}
}

// applyLabelCriterion applies label filtering using JSONB queries
func (re *RuleEngine) applyLabelCriterion(query *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	// Build JSONB path: metadata->'labels'->>'key'
	jsonPath := fmt.Sprintf("metadata->'labels'->>'%s'", criterion.Key)

	switch criterion.Operator {
	case "equals":
		return query.Where(fmt.Sprintf("%s = ?", jsonPath), criterion.Value)
	case "not_equals":
		return query.Where(fmt.Sprintf("%s != ?", jsonPath), criterion.Value)
	case "contains":
		return query.Where(fmt.Sprintf("%s LIKE ?", jsonPath), "%"+criterion.Value+"%")
	case "regex":
		// PostgreSQL regex operator
		return query.Where(fmt.Sprintf("%s ~ ?", jsonPath), criterion.Value)
	default:
		return query
	}
}

// applyAlertNameCriterion applies alert name filtering
func (re *RuleEngine) applyAlertNameCriterion(query *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	pattern := criterion.Pattern
	if pattern == "" {
		pattern = criterion.Value
	}

	switch criterion.Operator {
	case "equals":
		return query.Where("alert_name = ?", pattern)
	case "contains":
		return query.Where("alert_name LIKE ?", "%"+pattern+"%")
	case "starts_with":
		return query.Where("alert_name LIKE ?", pattern+"%")
	case "ends_with":
		return query.Where("alert_name LIKE ?", "%"+pattern)
	case "regex":
		// PostgreSQL regex operator
		return query.Where("alert_name ~ ?", pattern)
	default:
		return query
	}
}

// applyOrLogic builds a query with OR conditions
func (re *RuleEngine) applyOrLogic(query *gorm.DB, criteria []models.RuleCriterion) *gorm.DB {
	if len(criteria) == 0 {
		return query
	}

	// Build OR condition
	orQuery := query.Where(func(db *gorm.DB) *gorm.DB {
		for i, criterion := range criteria {
			if i == 0 {
				// First criterion - use Where
				db = re.applyCriterion(db, &criterion)
			} else {
				// Subsequent criteria - use Or
				db = db.Or(func(subDB *gorm.DB) *gorm.DB {
					return re.applyCriterion(subDB, &criterion)
				})
			}
		}
		return db
	})

	return orQuery
}

// ==================== Rule Testing ====================

// TestRule tests a rule against existing statistics
// Returns a sample of matching alerts and the total count
func (re *RuleEngine) TestRule(userID string, config *models.RuleConfig, sampleSize int) (matches []*models.AlertStatistic, totalCount int64, err error) {
	// Validate rule first
	if err := re.ValidateRule(config); err != nil {
		return nil, 0, fmt.Errorf("invalid rule: %w", err)
	}

	// Create a temporary rule for testing
	tempRule := &models.OnCallRule{
		UserID:     userID,
		RuleName:   "temp_test_rule",
		RuleConfig: nil, // Will be set below
		IsActive:   true,
	}

	// Convert config to JSONB
	ruleConfigJSON, err := database.BuildRuleConfigJSON(config)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build rule config: %w", err)
	}
	tempRule.RuleConfig = ruleConfigJSON

	// Temporarily save the rule (we'll delete it after)
	if err := re.db.SaveOnCallRule(tempRule); err != nil {
		return nil, 0, fmt.Errorf("failed to save temp rule: %w", err)
	}

	// Clean up temp rule after testing
	defer func() {
		_ = re.db.DeleteOnCallRule(tempRule.ID)
	}()

	// Build base query
	baseQuery := re.db.GetDB().Model(&models.AlertStatistic{})

	// Apply rule to query
	filteredQuery, err := re.ApplyRulesToQuery(userID, baseQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to apply rule: %w", err)
	}

	// Get total count
	if err := filteredQuery.Count(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count matches: %w", err)
	}

	// Get sample
	if sampleSize <= 0 {
		sampleSize = 10
	}

	var sampleMatches []*models.AlertStatistic
	if err := filteredQuery.Limit(sampleSize).Order("fired_at DESC").Find(&sampleMatches).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get sample: %w", err)
	}

	return sampleMatches, totalCount, nil
}

// ==================== Rule Management ====================

// GetRuleMatchCount returns the number of statistics that match a rule
// Useful for UI to show "This rule matches X alerts"
func (re *RuleEngine) GetRuleMatchCount(userID string, ruleID string) (int64, error) {
	// Load the rule
	rule, err := re.db.GetOnCallRuleByID(ruleID)
	if err != nil {
		return 0, fmt.Errorf("failed to load rule: %w", err)
	}

	// Verify ownership
	if rule.UserID != userID {
		return 0, fmt.Errorf("rule does not belong to user")
	}

	// Build base query
	baseQuery := re.db.GetDB().Model(&models.AlertStatistic{})

	// Apply rule
	filteredQuery, err := re.ApplyRulesToQuery(userID, baseQuery)
	if err != nil {
		return 0, fmt.Errorf("failed to apply rule: %w", err)
	}

	// Count matches
	var count int64
	if err := filteredQuery.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count: %w", err)
	}

	return count, nil
}

// ==================== Helper Functions ====================

// contains checks if a string slice contains a value
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
