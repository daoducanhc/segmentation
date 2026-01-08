package engine

import (
	"fmt"
	"strings"
	"time"

	v1 "segmentation/api/segment/v1"
)

// SQLGenerator generates ClickHouse SQL queries from segment definitions
type SQLGenerator struct {
	eventsTable   string
	usersTable    string
	activityTable string
}

// NewSQLGenerator creates a new SQL generator
func NewSQLGenerator() *SQLGenerator {
	return &SQLGenerator{
		eventsTable:   "segmentation.events",
		usersTable:    "segmentation.users",
		activityTable: "segmentation.user_daily_activity",
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
func (g *SQLGenerator) generateUserConditionsSQL(group *v1.ConditionGroup) (string, error) {
	whereClause, err := g.generateConditionGroupSQL(group)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT DISTINCT user_id FROM %s FINAL WHERE %s", g.usersTable, whereClause), nil
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

	if cond.Negated {
		result = "NOT (" + result + ")"
	}

	return result, nil
}

// generateEventConditionSQL generates SQL for an event condition
func (g *SQLGenerator) generateEventConditionSQL(eventCond *v1.EventCondition) (string, error) {
	if eventCond == nil {
		return "", nil
	}

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
