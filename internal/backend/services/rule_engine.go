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

// Constants for rule engine configuration
const (
	// DefaultTestSampleSize is the default number of alerts to return when testing a rule
	DefaultTestSampleSize = 10

	// MaxLabelKeyLength is the maximum allowed length for label keys
	MaxLabelKeyLength = 100
)

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

	// Validate label key to prevent SQL injection
	if err := sanitizeLabelKey(criterion.Key); err != nil {
		return fmt.Errorf("invalid label key: %w", err)
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
//
// Multiple active rules are combined with OR logic - alerts matching ANY active rule
// will be included in the results. Each individual rule applies its own internal logic
// (AND/OR for its criteria), then all rules are combined together with OR.
//
// Example with 2 active rules:
//   - Rule A: severity=critical AND team=platform
//   - Rule B: alert_name CONTAINS "database"
//   Result: (severity=critical AND team=platform) OR (alert_name LIKE '%database%')
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

	// Parse all rule configs
	var configs []*models.RuleConfig
	for _, rule := range rules {
		config, err := database.ParseRuleConfig(rule.RuleConfig)
		if err != nil {
			fmt.Printf("⚠️  WARNING: Failed to parse rule %s config: %v, skipping\n", rule.RuleName, err)
			continue
		}
		configs = append(configs, config)
	}

	// No valid configs after parsing
	if len(configs) == 0 {
		return baseQuery, nil
	}

	// Single rule - apply directly
	if len(configs) == 1 {
		return re.applyConfigToQuery(baseQuery, configs[0]), nil
	}

	// Multiple rules - combine with OR logic using GORM Group Conditions
	// Each rule becomes a grouped condition, all rules are OR'd together
	// Example: (rule1_cond1 AND rule1_cond2) OR (rule2_cond1 OR rule2_cond2)

	// Start with a fresh DB session to build OR conditions
	db := baseQuery.Session(&gorm.Session{NewDB: true})

	// Build the first rule's conditions
	orCondition := re.buildRuleConditionGroup(db, configs[0])

	// Add remaining rules with OR
	for i := 1; i < len(configs); i++ {
		ruleCondition := re.buildRuleConditionGroup(db, configs[i])
		orCondition = orCondition.Or(ruleCondition)
	}

	// Apply the combined OR conditions to the base query
	return baseQuery.Where(orCondition), nil
}

// buildRuleConditionGroup builds a GORM condition group for a single rule
func (re *RuleEngine) buildRuleConditionGroup(db *gorm.DB, config *models.RuleConfig) *gorm.DB {
	if len(config.Criteria) == 0 {
		return db
	}

	// Start with the first criterion
	result := re.applyCriterionToGroup(db, &config.Criteria[0])

	// Add remaining criteria based on rule's internal logic
	for i := 1; i < len(config.Criteria); i++ {
		criterion := &config.Criteria[i]
		if config.Logic == "OR" {
			result = result.Or(re.applyCriterionToGroup(db, criterion))
		} else {
			// AND logic - chain with Where
			result = re.applyCriterionToGroup(result, criterion)
		}
	}

	return result
}

// applyCriterionToGroup applies a single criterion and returns a GORM condition
func (re *RuleEngine) applyCriterionToGroup(db *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	switch criterion.Type {
	case "severity":
		return re.applySeverityCriterionGroup(db, criterion)
	case "label":
		return re.applyLabelCriterionGroup(db, criterion)
	case "alert_name":
		return re.applyAlertNameCriterionGroup(db, criterion)
	default:
		return db
	}
}

// applySeverityCriterionGroup applies severity filtering for group conditions
func (re *RuleEngine) applySeverityCriterionGroup(db *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	switch criterion.Operator {
	case "in":
		normalized := make([]string, len(criterion.Values))
		for i, v := range criterion.Values {
			normalized[i] = strings.ToLower(v)
		}
		return db.Where("LOWER(severity) IN ?", normalized)
	case "equals":
		return db.Where("LOWER(severity) = ?", strings.ToLower(criterion.Value))
	case "not_equals":
		return db.Where("LOWER(severity) != ?", strings.ToLower(criterion.Value))
	default:
		return db
	}
}

// applyLabelCriterionGroup applies label filtering for group conditions
func (re *RuleEngine) applyLabelCriterionGroup(db *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	if err := sanitizeLabelKey(criterion.Key); err != nil {
		fmt.Printf("ERROR: Invalid label key in stored rule: %v\n", err)
		return db
	}

	jsonPath := fmt.Sprintf("metadata->'labels'->>'%s'", criterion.Key)

	switch criterion.Operator {
	case "equals":
		return db.Where(fmt.Sprintf("%s = ?", jsonPath), criterion.Value)
	case "not_equals":
		return db.Where(fmt.Sprintf("%s != ?", jsonPath), criterion.Value)
	case "contains":
		return db.Where(fmt.Sprintf("%s LIKE ?", jsonPath), "%"+criterion.Value+"%")
	case "regex":
		return db.Where(fmt.Sprintf("%s ~ ?", jsonPath), criterion.Value)
	default:
		return db
	}
}

// applyAlertNameCriterionGroup applies alert name filtering for group conditions
func (re *RuleEngine) applyAlertNameCriterionGroup(db *gorm.DB, criterion *models.RuleCriterion) *gorm.DB {
	pattern := criterion.Pattern
	if pattern == "" {
		pattern = criterion.Value
	}

	switch criterion.Operator {
	case "equals":
		return db.Where("alert_name = ?", pattern)
	case "contains":
		return db.Where("alert_name LIKE ?", "%"+pattern+"%")
	case "starts_with":
		return db.Where("alert_name LIKE ?", pattern+"%")
	case "ends_with":
		return db.Where("alert_name LIKE ?", "%"+pattern)
	case "regex":
		return db.Where("alert_name ~ ?", pattern)
	default:
		return db
	}
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
	// Defense-in-depth: Validate label key before building query
	// This should have been validated during rule creation, but we check again for safety
	if err := sanitizeLabelKey(criterion.Key); err != nil {
		// Log error and return unmodified query (fail-safe behavior)
		fmt.Printf("ERROR: Invalid label key in stored rule: %v\n", err)
		return query
	}

	// Build JSONB path: metadata->'labels'->>'key'
	// criterion.Key is now validated, but we still use parameterization for values
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

// ==================== Rule Testing ====================

// TestRule tests a rule against existing statistics
// Returns a sample of matching alerts and the total count
func (re *RuleEngine) TestRule(userID string, config *models.RuleConfig, sampleSize int) (matches []*models.AlertStatistic, totalCount int64, err error) {
	// Validate rule first
	if err := re.ValidateRule(config); err != nil {
		return nil, 0, fmt.Errorf("invalid rule: %w", err)
	}

	// Build base query
	baseQuery := re.db.GetDB().Model(&models.AlertStatistic{})

	// Apply the config directly to the query (without saving to DB)
	filteredQuery := re.applyConfigToQuery(baseQuery, config)

	// Get total count
	if err := filteredQuery.Count(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count matches: %w", err)
	}

	// Get sample
	if sampleSize <= 0 {
		sampleSize = DefaultTestSampleSize
	}

	var sampleMatches []*models.AlertStatistic
	if err := filteredQuery.Limit(sampleSize).Order("fired_at DESC").Find(&sampleMatches).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get sample: %w", err)
	}

	return sampleMatches, totalCount, nil
}

// applyConfigToQuery applies a rule config directly to a query without loading from DB
func (re *RuleEngine) applyConfigToQuery(query *gorm.DB, config *models.RuleConfig) *gorm.DB {
	// Use GORM Group Conditions approach
	db := query.Session(&gorm.Session{NewDB: true})
	ruleCondition := re.buildRuleConditionGroup(db, config)
	return query.Where(ruleCondition)
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

// sanitizeLabelKey validates and sanitizes label keys to prevent SQL injection
// Only allows alphanumeric characters, underscores, hyphens, and dots
// Returns error if key contains invalid characters
func sanitizeLabelKey(key string) error {
	if key == "" {
		return fmt.Errorf("label key cannot be empty")
	}

	// Maximum length check (reasonable limit for label keys)
	if len(key) > MaxLabelKeyLength {
		return fmt.Errorf("label key too long (max %d characters)", MaxLabelKeyLength)
	}

	// Check for valid characters: alphanumeric, underscore, hyphen, dot
	// This prevents SQL injection via JSONB path manipulation
	validKeyPattern := regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)
	if !validKeyPattern.MatchString(key) {
		return fmt.Errorf("label key contains invalid characters (only alphanumeric, underscore, hyphen, and dot allowed): %s", key)
	}

	return nil
}
