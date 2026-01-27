// Package engine provides the segment evaluation engine.
package engine

import (
	"fmt"
	"strings"
	"time"

	v1 "segmentation/api/segment/v1"
)

// SQLGenerator generates ClickHouse SQL queries from segment definitions.
// Uses optimized table selection based on query complexity:
//   - user_activity_summary: Pre-computed flags (A7, A30, PU) - FASTEST
//   - user_daily_activity: Custom date ranges - FAST
//   - events: Complex property filters - SLOWER
type SQLGenerator struct {
	eventsTable   string
	usersTable    string
	activityTable string
	summaryTable  string
}

// NewSQLGenerator creates a new SQL generator
func NewSQLGenerator() *SQLGenerator {
	return &SQLGenerator{
		eventsTable:   "segmentation.events",
		usersTable:    "segmentation.users",
		activityTable: "segmentation.user_daily_activity",
		summaryTable:  "segmentation.user_activity_summary",
	}
}

// GenerateSQL generates a SQL query from a segment definition
func (g *SQLGenerator) GenerateSQL(def *v1.SegmentDefinition) (string, error) {
	if def == nil {
		return "", fmt.Errorf("segment definition is nil")
	}

	switch def.Type {
	case v1.SegmentType_SEGMENT_TYPE_DYNAMIC:
		return g.generateDynamicSQL(def)
	case v1.SegmentType_SEGMENT_TYPE_COMPOSITE:
		return g.generateCompositeSQL(def)
	case v1.SegmentType_SEGMENT_TYPE_STATIC:
		return "", fmt.Errorf("static segments should be read from cached results")
	default:
		return g.generateDynamicSQL(def)
	}
}

// generateDynamicSQL generates SQL for dynamic segments
func (g *SQLGenerator) generateDynamicSQL(def *v1.SegmentDefinition) (string, error) {
	var queries []string

	// Generate user conditions query if present
	if def.UserConditions != nil && (len(def.UserConditions.Conditions) > 0 || len(def.UserConditions.Groups) > 0) {
		userSQL, err := g.generateUserConditionsSQL(def.UserConditions)
		if err != nil {
			return "", err
		}
		queries = append(queries, userSQL)
	}

	// Generate event conditions queries
	for _, eventCond := range def.EventConditions {
		eventSQL, err := g.generateEventConditionSQL(eventCond)
		if err != nil {
			return "", err
		}
		queries = append(queries, eventSQL)
	}

	if len(queries) == 0 {
		// No conditions - return all users
		return fmt.Sprintf("SELECT DISTINCT user_id FROM %s FINAL", g.usersTable), nil
	}

	if len(queries) == 1 {
		return queries[0], nil
	}

	// Combine queries based on overall logic
	return g.combineQueries(queries, def.OverallLogic, def.EventLogic)
}

// generateCompositeSQL generates SQL for composite segments
func (g *SQLGenerator) generateCompositeSQL(def *v1.SegmentDefinition) (string, error) {
	if len(def.ChildSegments) == 0 {
		return "", fmt.Errorf("composite segment has no child segments")
	}

	var parts []string
	for _, child := range def.ChildSegments {
		subquery := fmt.Sprintf("SELECT user_id FROM segmentation.segment_results WHERE segment_id = '%s'", escapeSQLString(child.SegmentId))
		if child.Negated {
			subquery = fmt.Sprintf("SELECT user_id FROM %s FINAL EXCEPT %s", g.usersTable, subquery)
		}
		parts = append(parts, subquery)
	}

	if len(parts) == 1 {
		return parts[0], nil
	}

	op := "INTERSECT"
	if def.ChildLogic == v1.LogicalOperator_LOGICAL_OPERATOR_OR {
		op = "UNION DISTINCT"
	}

	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result = fmt.Sprintf("SELECT user_id FROM (%s) %s SELECT user_id FROM (%s)", result, op, parts[i])
	}

	return result, nil
}

// generateUserConditionsSQL generates SQL for user conditions
// Handles mixed fields from different tables (summary vs users) by:
// - Using summary table if it has all needed fields
// - Using users table if it has all needed fields
// - Using subqueries/joins for mixed fields
func (g *SQLGenerator) generateUserConditionsSQL(group *v1.ConditionGroup) (string, error) {
	// Analyze which tables are needed
	hasSummaryFields := g.needsSummaryTable(group)
	hasUserFields := g.needsUserTable(group)

	// If only one table is needed, use it directly
	if hasSummaryFields && !hasUserFields {
		whereClause, err := g.generateConditionGroupSQL(group)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("SELECT DISTINCT user_id FROM %s FINAL WHERE %s", g.summaryTable, whereClause), nil
	}

	if !hasSummaryFields && hasUserFields {
		whereClause, err := g.generateConditionGroupSQL(group)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("SELECT DISTINCT user_id FROM %s FINAL WHERE %s", g.usersTable, whereClause), nil
	}

	// Mixed fields - need to split conditions and join
	return g.generateMixedTableSQL(group)
}

// generateMixedTableSQL handles conditions that span both summary and users tables
func (g *SQLGenerator) generateMixedTableSQL(group *v1.ConditionGroup) (string, error) {
	// For OR operations at the top level, we can UNION the parts
	if group.Operator == v1.LogicalOperator_LOGICAL_OPERATOR_OR && len(group.Groups) > 0 {
		var parts []string
		for _, subGroup := range group.Groups {
			subSQL, err := g.generateUserConditionsSQL(subGroup)
			if err != nil {
				return "", err
			}
			parts = append(parts, "("+subSQL+")")
		}
		return fmt.Sprintf("SELECT DISTINCT user_id FROM (%s)", strings.Join(parts, " UNION ALL ")), nil
	}

	// For AND operations or single-level conditions, use subqueries
	whereClause, err := g.generateConditionGroupSQL(group)
	if err != nil {
		return "", err
	}

	// Use users table as base and join with summary table
	return fmt.Sprintf(`SELECT DISTINCT u.user_id 
FROM %s u FINAL
LEFT JOIN %s s FINAL ON u.user_id = s.user_id
WHERE %s`, g.usersTable, g.summaryTable, whereClause), nil
}

// needsUserTable checks if condition group uses users table fields
func (g *SQLGenerator) needsUserTable(group *v1.ConditionGroup) bool {
	if group == nil {
		return false
	}

	// Check direct conditions
	for _, cond := range group.Conditions {
		if !isSummaryField(cond.Field) {
			return true
		}
	}

	// Check nested groups
	for _, subGroup := range group.Groups {
		if g.needsUserTable(subGroup) {
			return true
		}
	}

	return false
}

// needsSummaryTable checks if condition group uses summary table fields
func (g *SQLGenerator) needsSummaryTable(group *v1.ConditionGroup) bool {
	if group == nil {
		return false
	}

	// Check direct conditions
	for _, cond := range group.Conditions {
		if isSummaryField(cond.Field) {
			return true
		}
	}

	// Check nested groups
	for _, subGroup := range group.Groups {
		if g.needsSummaryTable(subGroup) {
			return true
		}
	}

	return false
}

// isSummaryField checks if field belongs to user_activity_summary table
// Predefined lookback windows: 1, 3, 7, 30, 90 days
func isSummaryField(field string) bool {
	summaryFields := map[string]bool{
		// Activity flags: 1, 3, 7, 30, 90 days
		"is_active_1d":  true,
		"is_active_3d":  true,
		"is_active_7d":  true,
		"is_active_30d": true,
		"is_active_90d": true,
		// Paying user flags: 1, 3, 7, 30, 90 days
		"is_pu_1d":  true,
		"is_pu_3d":  true,
		"is_pu_7d":  true,
		"is_pu_30d": true,
		"is_pu_90d": true,
		// Churn flags: 1, 3, 7, 30, 90 days
		"is_churned_1d":  true,
		"is_churned_3d":  true,
		"is_churned_7d":  true,
		"is_churned_30d": true,
		"is_churned_90d": true,
	}
	return summaryFields[field]
}

// generateConditionGroupSQL generates SQL WHERE clause for a condition group
func (g *SQLGenerator) generateConditionGroupSQL(group *v1.ConditionGroup) (string, error) {
	if group == nil {
		return "1=1", nil
	}

	var parts []string

	// Process conditions
	for _, cond := range group.Conditions {
		condSQL, err := g.generateConditionSQL(cond)
		if err != nil {
			return "", err
		}
		if condSQL != "" {
			parts = append(parts, condSQL)
		}
	}

	// Process nested groups
	for _, subGroup := range group.Groups {
		subSQL, err := g.generateConditionGroupSQL(subGroup)
		if err != nil {
			return "", err
		}
		if subSQL != "" && subSQL != "1=1" {
			parts = append(parts, "("+subSQL+")")
		}
	}

	if len(parts) == 0 {
		return "1=1", nil
	}

	// Determine operator
	op := " AND "
	if group.Operator == v1.LogicalOperator_LOGICAL_OPERATOR_OR {
		op = " OR "
	}

	result := strings.Join(parts, op)

	// Apply NOT if negated
	if group.Negated {
		result = "NOT (" + result + ")"
	}

	return result, nil
}

// generateConditionSQL generates SQL for a single condition
func (g *SQLGenerator) generateConditionSQL(cond *v1.Condition) (string, error) {
	if cond == nil || cond.Field == "" {
		return "", nil
	}

	field := cond.Field
	op := g.comparisonOperatorToSQL(cond.Operator)
	value := g.conditionValueToSQL(cond.Value)

	var result string

	// Handle array fields (platform, country, language, os) stored as comma-separated strings
	if isArrayField(field) {
		// Extract raw string value for array operations
		rawValue := ""
		if cond.Value != nil {
			if sv, ok := cond.Value.Value.(*v1.ConditionValue_StringValue); ok {
				rawValue = sv.StringValue
			}
		}
		result = generateArrayFieldCondition(field, cond.Operator, rawValue, cond.Value)
	} else {
		// Standard field handling
		switch cond.Operator {
		case v1.ComparisonOperator_COMPARISON_OPERATOR_IS_NULL:
			result = fmt.Sprintf("%s IS NULL", field)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_IS_NOT_NULL:
			result = fmt.Sprintf("%s IS NOT NULL", field)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_IN, v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_IN:
			result = fmt.Sprintf("%s %s (%s)", field, op, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_BETWEEN:
			result = fmt.Sprintf("%s BETWEEN %s", field, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_CONTAINS:
			result = fmt.Sprintf("%s LIKE '%%%s%%'", field, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_CONTAINS:
			result = fmt.Sprintf("%s NOT LIKE '%%%s%%'", field, value)
		default:
			result = fmt.Sprintf("%s %s %s", field, op, value)
		}
	}

	if cond.Negated {
		result = "NOT (" + result + ")"
	}

	return result, nil
}

// generateEventConditionSQL generates SQL for an event condition
// Uses user_daily_activity for simple activity queries (faster)
// Falls back to events table for complex property filters
// IMPORTANT: eventName is REQUIRED. Use "*" for any event.
func (g *SQLGenerator) generateEventConditionSQL(eventCond *v1.EventCondition) (string, error) {
	if eventCond == nil {
		return "", nil
	}

	// Event name is REQUIRED
	if eventCond.EventName == "" {
		return "", fmt.Errorf("eventName is required. Use '*' for any event, 'app_page_view' for activity, 'pay' for purchases")
	}

	// Check if we can use the optimized daily activity table
	// Use daily activity for: any event (*), app_page_view, pay - without complex property filters
	hasPropertyFilter := eventCond.PropertyFilter != nil &&
		(len(eventCond.PropertyFilter.Conditions) > 0 || len(eventCond.PropertyFilter.Groups) > 0)

	canUseDailyTable := !hasPropertyFilter &&
		(eventCond.EventName == "*" ||
			eventCond.EventName == "app_page_view" || eventCond.EventName == "pay" ||
			eventCond.EventName == "churned")

	if canUseDailyTable {
		return g.generateActivityConditionSQL(eventCond)
	}

	// Fall back to events table for complex queries
	return g.generateRawEventConditionSQL(eventCond)
}

// generateActivityConditionSQL uses optimized tables based on lookback days
// - Predefined (7, 30, 90 days): uses flags (is_active_7d, etc.) - FAST
// - Everything else: uses daily activity table - Good enough
func (g *SQLGenerator) generateActivityConditionSQL(eventCond *v1.EventCondition) (string, error) {
	// Check if we can use pre-computed flags for predefined lookback days
	if g.canUseSummaryTable(eventCond) {
		return g.generateSummaryConditionSQL(eventCond)
	}

	// Use daily activity table for custom lookback days or complex queries
	return g.generateDailyActivityConditionSQL(eventCond)
}

// canUseSummaryTable checks if we can use the pre-computed summary table
// Predefined lookback windows: 1, 3, 7, 30, 90 days
func (g *SQLGenerator) canUseSummaryTable(eventCond *v1.EventCondition) bool {
	// Only predefined windows (1, 3, 7, 30, 90 days) have pre-computed flags
	isPredefinedWindow := eventCond.LookbackDays == 1 ||
		eventCond.LookbackDays == 3 ||
		eventCond.LookbackDays == 7 ||
		eventCond.LookbackDays == 30 ||
		eventCond.LookbackDays == 90

	if !isPredefinedWindow {
		return false
	}

	// For churned criteria, we check for "no activity" so count condition doesn't apply
	if eventCond.EventName == "churned" {
		return true
	}

	// For activity/PU, only simple count >= 1 condition
	if eventCond.CountOperator != v1.ComparisonOperator_COMPARISON_OPERATOR_GTE {
		return false
	}
	if eventCond.CountValue != 1 {
		return false
	}

	return true
}

// generateSummaryConditionSQL uses pre-computed user_activity_summary (FAST)
// Supports predefined windows: 1, 3, 7, 30, 90 days
// Event types: "*" or "app_page_view" (activity), "pay" (PU), "churned"
func (g *SQLGenerator) generateSummaryConditionSQL(eventCond *v1.EventCondition) (string, error) {
	var condition string

	switch eventCond.EventName {
	case "pay":
		// Paying user criteria
		switch eventCond.LookbackDays {
		case 1:
			condition = "is_pu_1d = 1"
		case 3:
			condition = "is_pu_3d = 1"
		case 7:
			condition = "is_pu_7d = 1"
		case 30:
			condition = "is_pu_30d = 1"
		case 90:
			condition = "is_pu_90d = 1"
		default:
			return "", fmt.Errorf("unsupported lookback days for PU: %d (allowed: 1, 3, 7, 30, 90)", eventCond.LookbackDays)
		}
	case "churned":
		// Churned criteria (no activity in N+ days)
		switch eventCond.LookbackDays {
		case 1:
			condition = "is_churned_1d = 1"
		case 3:
			condition = "is_churned_3d = 1"
		case 7:
			condition = "is_churned_7d = 1"
		case 30:
			condition = "is_churned_30d = 1"
		case 90:
			condition = "is_churned_90d = 1"
		default:
			return "", fmt.Errorf("unsupported lookback days for churned: %d (allowed: 1, 3, 7, 30, 90)", eventCond.LookbackDays)
		}
	case "*", "app_page_view":
		// Activity criteria (any event or app_page_view)
		switch eventCond.LookbackDays {
		case 1:
			condition = "is_active_1d = 1"
		case 3:
			condition = "is_active_3d = 1"
		case 7:
			condition = "is_active_7d = 1"
		case 30:
			condition = "is_active_30d = 1"
		case 90:
			condition = "is_active_90d = 1"
		default:
			return "", fmt.Errorf("unsupported lookback days for activity: %d (allowed: 1, 3, 7, 30, 90)", eventCond.LookbackDays)
		}
	default:
		return "", fmt.Errorf("unsupported event name for summary table: %s (allowed: '*', 'app_page_view', 'pay', 'churned')", eventCond.EventName)
	}

	// Use FINAL to get deduplicated results from ReplacingMergeTree
	return fmt.Sprintf(`
				SELECT user_id
				FROM %s FINAL
				WHERE %s`, g.summaryTable, condition), nil
}

// generateDailyActivityConditionSQL uses user_daily_activity for custom lookback
// Event name is REQUIRED: "*" (any event), "app_page_view", "pay"
func (g *SQLGenerator) generateDailyActivityConditionSQL(eventCond *v1.EventCondition) (string, error) {
	var whereParts []string

	// Time window (lookback days)
	if eventCond.LookbackDays > 0 {
		whereParts = append(whereParts, fmt.Sprintf("activity_date >= today() - %d", eventCond.LookbackDays))
	}

	whereClause := "1=1"
	if len(whereParts) > 0 {
		whereClause = strings.Join(whereParts, " AND ")
	}

	// Determine which metric to aggregate based on event type
	var havingClause string
	op := g.comparisonOperatorToSQL(eventCond.CountOperator)
	if op == "" || op == "=" {
		op = ">="
	}

	switch eventCond.EventName {
	case "app_page_view":
		// Use login_count from daily activity
		havingClause = fmt.Sprintf("sum(login_count) %s %d", op, eventCond.CountValue)
	case "pay":
		// Use purchase_count from daily activity
		havingClause = fmt.Sprintf("sum(purchase_count) %s %d", op, eventCond.CountValue)
	case "*":
		// Any event - use total event_count
		havingClause = fmt.Sprintf("sum(event_count) %s %d", op, eventCond.CountValue)
	default:
		return "", fmt.Errorf("unsupported event name for daily activity: %s (allowed: '*', 'app_page_view', 'pay')", eventCond.EventName)
	}

	return fmt.Sprintf(`
				SELECT user_id
				FROM %s
				WHERE %s
				GROUP BY user_id
				HAVING %s`, g.activityTable, whereClause, havingClause), nil
}

// generateRawEventConditionSQL uses events table for complex queries
func (g *SQLGenerator) generateRawEventConditionSQL(eventCond *v1.EventCondition) (string, error) {
	var whereParts []string

	// Event name filter
	if eventCond.EventName != "" && eventCond.EventName != "*" {
		whereParts = append(whereParts, fmt.Sprintf("event_name = '%s'", escapeSQLString(eventCond.EventName)))
	}

	// Time window (lookback days)
	if eventCond.LookbackDays > 0 {
		whereParts = append(whereParts, fmt.Sprintf("event_date >= today() - %d", eventCond.LookbackDays))
	}

	// Property filter
	if eventCond.PropertyFilter != nil && (len(eventCond.PropertyFilter.Conditions) > 0 || len(eventCond.PropertyFilter.Groups) > 0) {
		propSQL, err := g.generatePropertyFilterSQL(eventCond.PropertyFilter)
		if err != nil {
			return "", err
		}
		if propSQL != "" && propSQL != "1=1" {
			whereParts = append(whereParts, propSQL)
		}
	}

	whereClause := "1=1"
	if len(whereParts) > 0 {
		whereClause = strings.Join(whereParts, " AND ")
	}

	// Build the query based on count operator
	op := g.comparisonOperatorToSQL(eventCond.CountOperator)
	if op == "" || op == "=" {
		op = ">="
	}

	return fmt.Sprintf(`
SELECT user_id
FROM %s
WHERE %s
GROUP BY user_id
HAVING count() %s %d`, g.eventsTable, whereClause, op, eventCond.CountValue), nil
}

// generatePropertyFilterSQL generates SQL for event property filters
func (g *SQLGenerator) generatePropertyFilterSQL(group *v1.ConditionGroup) (string, error) {
	if group == nil {
		return "1=1", nil
	}

	var parts []string

	for _, cond := range group.Conditions {
		if cond == nil || cond.Field == "" {
			continue
		}

		// Use JSONExtract for event properties
		valueExpr := fmt.Sprintf("JSONExtractString(properties, '%s')", escapeSQLString(cond.Field))
		value := g.conditionValueToSQL(cond.Value)

		var condSQL string
		switch cond.Operator {
		case v1.ComparisonOperator_COMPARISON_OPERATOR_EQ:
			condSQL = fmt.Sprintf("%s = %s", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_NEQ:
			condSQL = fmt.Sprintf("%s != %s", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_GT:
			condSQL = fmt.Sprintf("toFloat64OrZero(%s) > %s", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_GTE:
			condSQL = fmt.Sprintf("toFloat64OrZero(%s) >= %s", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_LT:
			condSQL = fmt.Sprintf("toFloat64OrZero(%s) < %s", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_LTE:
			condSQL = fmt.Sprintf("toFloat64OrZero(%s) <= %s", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_CONTAINS:
			condSQL = fmt.Sprintf("%s LIKE '%%%s%%'", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_CONTAINS:
			condSQL = fmt.Sprintf("%s NOT LIKE '%%%s%%'", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_IN:
			condSQL = fmt.Sprintf("%s IN (%s)", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_IN:
			condSQL = fmt.Sprintf("%s NOT IN (%s)", valueExpr, value)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_IS_NULL:
			condSQL = fmt.Sprintf("%s = ''", valueExpr)
		case v1.ComparisonOperator_COMPARISON_OPERATOR_IS_NOT_NULL:
			condSQL = fmt.Sprintf("%s != ''", valueExpr)
		default:
			condSQL = fmt.Sprintf("%s = %s", valueExpr, value)
		}

		if cond.Negated {
			condSQL = "NOT (" + condSQL + ")"
		}

		parts = append(parts, condSQL)
	}

	// Process nested groups
	for _, subGroup := range group.Groups {
		subSQL, err := g.generatePropertyFilterSQL(subGroup)
		if err != nil {
			return "", err
		}
		if subSQL != "" && subSQL != "1=1" {
			parts = append(parts, "("+subSQL+")")
		}
	}

	if len(parts) == 0 {
		return "1=1", nil
	}

	op := " AND "
	if group.Operator == v1.LogicalOperator_LOGICAL_OPERATOR_OR {
		op = " OR "
	}

	result := strings.Join(parts, op)
	if group.Negated {
		result = "NOT (" + result + ")"
	}

	return result, nil
}

// combineQueries combines multiple queries with logical operators
func (g *SQLGenerator) combineQueries(queries []string, overallLogic, eventLogic v1.LogicalOperator) (string, error) {
	if len(queries) == 0 {
		return "", fmt.Errorf("no queries to combine")
	}

	if len(queries) == 1 {
		return queries[0], nil
	}

	logic := overallLogic
	if logic == v1.LogicalOperator_LOGICAL_OPERATOR_UNSPECIFIED {
		logic = eventLogic
	}
	if logic == v1.LogicalOperator_LOGICAL_OPERATOR_UNSPECIFIED {
		logic = v1.LogicalOperator_LOGICAL_OPERATOR_AND
	}

	op := "INTERSECT"
	if logic == v1.LogicalOperator_LOGICAL_OPERATOR_OR {
		op = "UNION DISTINCT"
	}

	result := fmt.Sprintf("SELECT user_id FROM (%s)", queries[0])
	for i := 1; i < len(queries); i++ {
		result = fmt.Sprintf("%s %s SELECT user_id FROM (%s)", result, op, queries[i])
	}

	return result, nil
}

// GenerateCountSQL generates a count query
func (g *SQLGenerator) GenerateCountSQL(def *v1.SegmentDefinition) (string, error) {
	sql, err := g.GenerateSQL(def)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT count() FROM (%s)", sql), nil
}

// comparisonOperatorToSQL converts a ComparisonOperator to SQL
func (g *SQLGenerator) comparisonOperatorToSQL(op v1.ComparisonOperator) string {
	switch op {
	case v1.ComparisonOperator_COMPARISON_OPERATOR_EQ:
		return "="
	case v1.ComparisonOperator_COMPARISON_OPERATOR_NEQ:
		return "!="
	case v1.ComparisonOperator_COMPARISON_OPERATOR_GT:
		return ">"
	case v1.ComparisonOperator_COMPARISON_OPERATOR_GTE:
		return ">="
	case v1.ComparisonOperator_COMPARISON_OPERATOR_LT:
		return "<"
	case v1.ComparisonOperator_COMPARISON_OPERATOR_LTE:
		return "<="
	case v1.ComparisonOperator_COMPARISON_OPERATOR_IN:
		return "IN"
	case v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_IN:
		return "NOT IN"
	case v1.ComparisonOperator_COMPARISON_OPERATOR_BETWEEN:
		return "BETWEEN"
	case v1.ComparisonOperator_COMPARISON_OPERATOR_CONTAINS:
		return "LIKE"
	case v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_CONTAINS:
		return "NOT LIKE"
	default:
		return "="
	}
}

// conditionValueToSQL converts a ConditionValue to SQL string
func (g *SQLGenerator) conditionValueToSQL(val *v1.ConditionValue) string {
	if val == nil {
		return "''"
	}

	switch v := val.Value.(type) {
	case *v1.ConditionValue_StringValue:
		return fmt.Sprintf("'%s'", escapeSQLString(v.StringValue))
	case *v1.ConditionValue_IntValue:
		return fmt.Sprintf("%d", v.IntValue)
	case *v1.ConditionValue_DoubleValue:
		return fmt.Sprintf("%f", v.DoubleValue)
	case *v1.ConditionValue_BoolValue:
		if v.BoolValue {
			return "1"
		}
		return "0"
	case *v1.ConditionValue_StringList:
		if v.StringList != nil && len(v.StringList.Values) > 0 {
			quoted := make([]string, len(v.StringList.Values))
			for i, s := range v.StringList.Values {
				quoted[i] = fmt.Sprintf("'%s'", escapeSQLString(s))
			}
			return strings.Join(quoted, ", ")
		}
	case *v1.ConditionValue_IntList:
		if v.IntList != nil && len(v.IntList.Values) > 0 {
			nums := make([]string, len(v.IntList.Values))
			for i, n := range v.IntList.Values {
				nums[i] = fmt.Sprintf("%d", n)
			}
			return strings.Join(nums, ", ")
		}
	case *v1.ConditionValue_DoubleRange:
		if v.DoubleRange != nil {
			return fmt.Sprintf("%f AND %f", v.DoubleRange.Min, v.DoubleRange.Max)
		}
	case *v1.ConditionValue_IntRange:
		if v.IntRange != nil {
			return fmt.Sprintf("%d AND %d", v.IntRange.Min, v.IntRange.Max)
		}
	case *v1.ConditionValue_DateRange:
		if v.DateRange != nil {
			start := ""
			end := ""
			if v.DateRange.Start != nil {
				start = v.DateRange.Start.AsTime().Format("2006-01-02")
			}
			if v.DateRange.End != nil {
				end = v.DateRange.End.AsTime().Format("2006-01-02")
			}
			return fmt.Sprintf("'%s' AND '%s'", start, end)
		}
	}

	return "''"
}

// escapeSQLString escapes a string for safe SQL insertion
func escapeSQLString(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return s
}

// isArrayField returns true if the field stores comma-separated values
func isArrayField(field string) bool {
	arrayFields := map[string]bool{
		"platform": true,
		"country":  true,
		"language": true,
		"os":       true,
	}
	return arrayFields[field]
}

// generateArrayFieldCondition generates SQL for comma-separated array fields
// Example: platform = 'web_mobile,app' and we want to check if 'web_mobile' is in it
func generateArrayFieldCondition(field string, op v1.ComparisonOperator, value string, condValue *v1.ConditionValue) string {
	// Convert comma-separated string to array and check membership
	arrayExpr := fmt.Sprintf("splitByChar(',', %s)", field)

	switch op {
	case v1.ComparisonOperator_COMPARISON_OPERATOR_EQ:
		// "has platform web_mobile" -> check if value is in the array
		return fmt.Sprintf("has(%s, '%s')", arrayExpr, escapeSQLString(value))
	case v1.ComparisonOperator_COMPARISON_OPERATOR_NEQ:
		// "not has platform web_mobile"
		return fmt.Sprintf("NOT has(%s, '%s')", arrayExpr, escapeSQLString(value))
	case v1.ComparisonOperator_COMPARISON_OPERATOR_IN:
		// "platform in ['web_mobile', 'app']" -> check if any value matches
		values := extractStringList(condValue)
		if len(values) > 0 {
			quoted := make([]string, len(values))
			for i, v := range values {
				quoted[i] = fmt.Sprintf("'%s'", escapeSQLString(v))
			}
			return fmt.Sprintf("hasAny(%s, [%s])", arrayExpr, strings.Join(quoted, ", "))
		}
		return fmt.Sprintf("has(%s, '%s')", arrayExpr, escapeSQLString(value))
	case v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_IN:
		values := extractStringList(condValue)
		if len(values) > 0 {
			quoted := make([]string, len(values))
			for i, v := range values {
				quoted[i] = fmt.Sprintf("'%s'", escapeSQLString(v))
			}
			return fmt.Sprintf("NOT hasAny(%s, [%s])", arrayExpr, strings.Join(quoted, ", "))
		}
		return fmt.Sprintf("NOT has(%s, '%s')", arrayExpr, escapeSQLString(value))
	case v1.ComparisonOperator_COMPARISON_OPERATOR_CONTAINS:
		// Substring match
		return fmt.Sprintf("%s LIKE '%%%s%%'", field, escapeSQLString(value))
	case v1.ComparisonOperator_COMPARISON_OPERATOR_NOT_CONTAINS:
		return fmt.Sprintf("%s NOT LIKE '%%%s%%'", field, escapeSQLString(value))
	default:
		// Fallback to simple has check
		return fmt.Sprintf("has(%s, '%s')", arrayExpr, escapeSQLString(value))
	}
}

// extractStringList extracts string list from ConditionValue
func extractStringList(val *v1.ConditionValue) []string {
	if val == nil {
		return nil
	}
	if sl, ok := val.Value.(*v1.ConditionValue_StringList); ok && sl.StringList != nil {
		return sl.StringList.Values
	}
	return nil
}

// ValidateDefinition validates a segment definition
func (g *SQLGenerator) ValidateDefinition(def *v1.SegmentDefinition) error {
	if def == nil {
		return fmt.Errorf("segment definition is nil")
	}

	switch def.Type {
	case v1.SegmentType_SEGMENT_TYPE_COMPOSITE:
		if len(def.ChildSegments) == 0 {
			return fmt.Errorf("composite segment requires child segments")
		}
	case v1.SegmentType_SEGMENT_TYPE_DYNAMIC, v1.SegmentType_SEGMENT_TYPE_UNSPECIFIED:
		// Dynamic or default - need either user conditions or event conditions
		hasUserConditions := def.UserConditions != nil && (len(def.UserConditions.Conditions) > 0 || len(def.UserConditions.Groups) > 0)
		hasEventConditions := len(def.EventConditions) > 0
		if !hasUserConditions && !hasEventConditions {
			return fmt.Errorf("dynamic segment requires at least one condition")
		}
	}

	return nil
}

// GetTimeRange returns the time range from a segment definition
func (g *SQLGenerator) GetTimeRange(def *v1.SegmentDefinition) (start, end time.Time) {
	now := time.Now()
	start = now.AddDate(0, 0, -30) // Default to last 30 days
	end = now

	if def == nil {
		return
	}

	// Find max lookback from event conditions
	for _, eventCond := range def.EventConditions {
		if eventCond.LookbackDays > 0 {
			candidateStart := now.AddDate(0, 0, -int(eventCond.LookbackDays))
			if candidateStart.Before(start) {
				start = candidateStart
			}
		}
	}

	return
}
