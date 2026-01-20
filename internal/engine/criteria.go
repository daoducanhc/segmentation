package engine

import (
	"fmt"

	v1 "segmentation/api/segment/v1"
)

// Criterion represents a single filtering criterion (e.g., A7, A30, PU, Platform)
// Criteria are the building blocks - segments are combinations of criteria using AND/OR/NOT
type Criterion struct {
	Name           string
	Description    string
	UserConditions *v1.ConditionGroup
	EventCondition *v1.EventCondition
}

// CriteriaLibrary provides factory functions for predefined criteria
// Criteria are building blocks (A7, A30, PU, Platform, etc.)
// Segments are combinations of criteria using AND/OR/NOT logic
type CriteriaLibrary struct{}

// NewCriteriaLibrary creates a new CriteriaLibrary instance
func NewCriteriaLibrary() *CriteriaLibrary {
	return &CriteriaLibrary{}
}

// ========== Activity Criteria ==========

// A7 returns the "Active in Last 7 Days" criterion
func (c *CriteriaLibrary) A7() *Criterion {
	return &Criterion{
		Name:        "A7",
		Description: "Users active in the last 7 days",
		EventCondition: &v1.EventCondition{
			EventName:     "*",
			LookbackDays:  7,
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    1,
		},
	}
}

// A30 returns the "Active in Last 30 Days" criterion
func (c *CriteriaLibrary) A30() *Criterion {
	return &Criterion{
		Name:        "A30",
		Description: "Users active in the last 30 days",
		EventCondition: &v1.EventCondition{
			EventName:     "*",
			LookbackDays:  30,
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    1,
		},
	}
}

// ActiveNDays returns an "Active in Last N Days" criterion
func (c *CriteriaLibrary) ActiveNDays(days int32) *Criterion {
	return &Criterion{
		Name:        fmt.Sprintf("A%d", days),
		Description: fmt.Sprintf("Users active in the last %d days", days),
		EventCondition: &v1.EventCondition{
			EventName:     "*",
			LookbackDays:  days,
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    1,
		},
	}
}

// ========== Payment Criteria ==========

// PayingUsers returns the "Paying Users (PU)" criterion
func (c *CriteriaLibrary) PayingUsers() *Criterion {
	return &Criterion{
		Name:        "PU",
		Description: "Users who have made at least one purchase",
		UserConditions: &v1.ConditionGroup{
			Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
			Conditions: []*v1.Condition{
				{
					Field:    "is_paying_user",
					Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
					Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: 1}},
				},
			},
		},
	}
}

// NonPayingUsers returns the "Non-Paying Users (NPU)" criterion
func (c *CriteriaLibrary) NonPayingUsers() *Criterion {
	return &Criterion{
		Name:        "NPU",
		Description: "Users who have never made a purchase",
		UserConditions: &v1.ConditionGroup{
			Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
			Conditions: []*v1.Condition{
				{
					Field:    "is_paying_user",
					Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
					Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: 0}},
				},
			},
		},
	}
}

// HighValue returns the "High Value Users" criterion
func (c *CriteriaLibrary) HighValue(revenueThreshold float64) *Criterion {
	return &Criterion{
		Name:        "HIGH_VALUE",
		Description: fmt.Sprintf("Users with total revenue >= %.2f", revenueThreshold),
		UserConditions: &v1.ConditionGroup{
			Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
			Conditions: []*v1.Condition{
				{
					Field:    "total_revenue",
					Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
					Value:    &v1.ConditionValue{Value: &v1.ConditionValue_DoubleValue{DoubleValue: revenueThreshold}},
				},
			},
		},
	}
}

// ========== Lifecycle Criteria ==========

// NewUsers returns the "New Users" criterion
func (c *CriteriaLibrary) NewUsers(days int32) *Criterion {
	return &Criterion{
		Name:        "NEW_USERS",
		Description: fmt.Sprintf("Users who joined in the last %d days", days),
		UserConditions: &v1.ConditionGroup{
			Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
			Conditions: []*v1.Condition{
				{
					Field:    fmt.Sprintf("first_seen_at >= now() - INTERVAL %d DAY", days),
					Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
					Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: 1}},
				},
			},
		},
	}
}

// ChurnedUsers returns the "Churned Users" criterion
func (c *CriteriaLibrary) ChurnedUsers(inactiveDays int32) *Criterion {
	return &Criterion{
		Name:        "CHURNED",
		Description: fmt.Sprintf("Users inactive for %d+ days", inactiveDays),
		UserConditions: &v1.ConditionGroup{
			Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
			Conditions: []*v1.Condition{
				{
					Field:    fmt.Sprintf("last_seen_at < now() - INTERVAL %d DAY", inactiveDays),
					Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
					Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: 1}},
				},
			},
		},
	}
}

// ========== Platform/Demographic Criteria ==========

// Platform returns a "Platform" criterion
func (c *CriteriaLibrary) Platform(platform string) *Criterion {
	return &Criterion{
		Name:        "PLATFORM",
		Description: fmt.Sprintf("Users on %s platform", platform),
		UserConditions: &v1.ConditionGroup{
			Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
			Conditions: []*v1.Condition{
				{
					Field:    "platform",
					Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
					Value:    &v1.ConditionValue{Value: &v1.ConditionValue_StringValue{StringValue: platform}},
				},
			},
		},
	}
}

// Country returns a "Country" criterion
func (c *CriteriaLibrary) Country(country string) *Criterion {
	return &Criterion{
		Name:        "COUNTRY",
		Description: fmt.Sprintf("Users from %s", country),
		UserConditions: &v1.ConditionGroup{
			Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
			Conditions: []*v1.Condition{
				{
					Field:    "country",
					Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
					Value:    &v1.ConditionValue{Value: &v1.ConditionValue_StringValue{StringValue: country}},
				},
			},
		},
	}
}

// ========== VIP Level Criteria ==========

// VIPLevel returns a criterion for users at a specific VIP level for an app
func (c *CriteriaLibrary) VIPLevel(appID string, level uint8) *Criterion {
	return &Criterion{
		Name:        "VIP_LEVEL",
		Description: fmt.Sprintf("Users at VIP level %d for app %s", level, appID),
		EventCondition: &v1.EventCondition{
			EventName:    "app_vip_level_up",
			LookbackDays: 365, // Look at historical VIP data
			PropertyFilter: &v1.ConditionGroup{
				Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
				Conditions: []*v1.Condition{
					{
						Field:    "app_id",
						Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_StringValue{StringValue: appID}},
					},
					{
						Field:    "vip_level",
						Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: int64(level)}},
					},
				},
			},
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    1,
		},
	}
}

// VIPLevelAtLeast returns a criterion for users at or above a specific VIP level for an app
func (c *CriteriaLibrary) VIPLevelAtLeast(appID string, minLevel uint8) *Criterion {
	return &Criterion{
		Name:        "VIP_LEVEL_GTE",
		Description: fmt.Sprintf("Users at VIP level >= %d for app %s", minLevel, appID),
		EventCondition: &v1.EventCondition{
			EventName:    "app_vip_level_up",
			LookbackDays: 365,
			PropertyFilter: &v1.ConditionGroup{
				Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
				Conditions: []*v1.Condition{
					{
						Field:    "app_id",
						Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_StringValue{StringValue: appID}},
					},
					{
						Field:    "vip_level",
						Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: int64(minLevel)}},
					},
				},
			},
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    1,
		},
	}
}

// VIPLevelRange returns a criterion for users within a VIP level range for an app
func (c *CriteriaLibrary) VIPLevelRange(appID string, minLevel, maxLevel uint8) *Criterion {
	return &Criterion{
		Name:        "VIP_LEVEL_RANGE",
		Description: fmt.Sprintf("Users at VIP level %d-%d for app %s", minLevel, maxLevel, appID),
		EventCondition: &v1.EventCondition{
			EventName:    "app_vip_level_up",
			LookbackDays: 365,
			PropertyFilter: &v1.ConditionGroup{
				Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
				Conditions: []*v1.Condition{
					{
						Field:    "app_id",
						Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_StringValue{StringValue: appID}},
					},
					{
						Field:    "vip_level",
						Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: int64(minLevel)}},
					},
					{
						Field:    "vip_level",
						Operator: v1.ComparisonOperator_COMPARISON_OPERATOR_LTE,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_IntValue{IntValue: int64(maxLevel)}},
					},
				},
			},
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    1,
		},
	}
}

// ========== Event Criteria ==========

// EventPerformed returns a criterion for users who performed a specific event
func (c *CriteriaLibrary) EventPerformed(eventName string, days int32, minCount int64) *Criterion {
	return &Criterion{
		Name:        "EVENT_PERFORMED",
		Description: fmt.Sprintf("Users who performed '%s' at least %d times in %d days", eventName, minCount, days),
		EventCondition: &v1.EventCondition{
			EventName:     eventName,
			LookbackDays:  days,
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    minCount,
		},
	}
}

// EventNotPerformed returns a criterion for users who did NOT perform a specific event
func (c *CriteriaLibrary) EventNotPerformed(eventName string, days int32) *Criterion {
	return &Criterion{
		Name:        "EVENT_NOT_PERFORMED",
		Description: fmt.Sprintf("Users who did NOT perform '%s' in %d days", eventName, days),
		EventCondition: &v1.EventCondition{
			EventName:     eventName,
			LookbackDays:  days,
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_EQ,
			CountValue:    0,
		},
	}
}

// EventWithProperty returns a criterion for users who performed an event with specific property
func (c *CriteriaLibrary) EventWithProperty(eventName, propertyName string, operator v1.ComparisonOperator, value string, days int32) *Criterion {
	return &Criterion{
		Name:        "EVENT_WITH_PROPERTY",
		Description: fmt.Sprintf("Users who performed '%s' with %s filter in %d days", eventName, propertyName, days),
		EventCondition: &v1.EventCondition{
			EventName:    eventName,
			LookbackDays: days,
			PropertyFilter: &v1.ConditionGroup{
				Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
				Conditions: []*v1.Condition{
					{
						Field:    propertyName,
						Operator: operator,
						Value:    &v1.ConditionValue{Value: &v1.ConditionValue_StringValue{StringValue: value}},
					},
				},
			},
			CountOperator: v1.ComparisonOperator_COMPARISON_OPERATOR_GTE,
			CountValue:    1,
		},
	}
}

// ========== Segment Builder (combines criteria into segments) ==========

// SegmentBuilder builds segments from criteria using AND/OR/NOT logic
type SegmentBuilder struct {
	criteria *CriteriaLibrary
}

// NewSegmentBuilder creates a new SegmentBuilder
func NewSegmentBuilder(criteria *CriteriaLibrary) *SegmentBuilder {
	return &SegmentBuilder{criteria: criteria}
}

// BuildSegment creates a segment definition from criteria with AND logic
func (b *SegmentBuilder) BuildSegment(criteria ...*Criterion) *v1.SegmentDefinition {
	return b.BuildSegmentAND(criteria...)
}

// BuildSegmentAND creates a segment from criteria combined with AND logic
// Example: A7 AND PU AND Platform("app") = Active paying app users
func (b *SegmentBuilder) BuildSegmentAND(criteria ...*Criterion) *v1.SegmentDefinition {
	def := &v1.SegmentDefinition{
		Type:         v1.SegmentType_SEGMENT_TYPE_DYNAMIC,
		OverallLogic: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
		EventLogic:   v1.LogicalOperator_LOGICAL_OPERATOR_AND,
	}

	for _, c := range criteria {
		if c.UserConditions != nil {
			if def.UserConditions == nil {
				def.UserConditions = &v1.ConditionGroup{
					Operator: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
				}
			}
			def.UserConditions.Groups = append(def.UserConditions.Groups, c.UserConditions)
		}
		if c.EventCondition != nil {
			def.EventConditions = append(def.EventConditions, c.EventCondition)
		}
	}

	return def
}

// BuildSegmentOR creates a segment from criteria combined with OR logic
// Example: Platform("web_mobile") OR Platform("app") = Mobile users
func (b *SegmentBuilder) BuildSegmentOR(criteria ...*Criterion) *v1.SegmentDefinition {
	def := &v1.SegmentDefinition{
		Type:         v1.SegmentType_SEGMENT_TYPE_DYNAMIC,
		OverallLogic: v1.LogicalOperator_LOGICAL_OPERATOR_OR,
		EventLogic:   v1.LogicalOperator_LOGICAL_OPERATOR_OR,
	}

	for _, c := range criteria {
		if c.UserConditions != nil {
			if def.UserConditions == nil {
				def.UserConditions = &v1.ConditionGroup{
					Operator: v1.LogicalOperator_LOGICAL_OPERATOR_OR,
				}
			}
			def.UserConditions.Groups = append(def.UserConditions.Groups, c.UserConditions)
		}
		if c.EventCondition != nil {
			def.EventConditions = append(def.EventConditions, c.EventCondition)
		}
	}

	return def
}

// BuildSegmentNOT creates a segment that negates a criterion
// Example: NOT PU = Non-paying users
func (b *SegmentBuilder) BuildSegmentNOT(criterion *Criterion) *v1.SegmentDefinition {
	def := &v1.SegmentDefinition{
		Type:         v1.SegmentType_SEGMENT_TYPE_DYNAMIC,
		OverallLogic: v1.LogicalOperator_LOGICAL_OPERATOR_AND,
	}

	if criterion.UserConditions != nil {
		negatedConditions := &v1.ConditionGroup{
			Operator: criterion.UserConditions.Operator,
			Negated:  true,
		}
		// Copy conditions with negation
		for _, cond := range criterion.UserConditions.Conditions {
			negatedCond := &v1.Condition{
				Field:    cond.Field,
				Operator: cond.Operator,
				Value:    cond.Value,
				Negated:  true,
			}
			negatedConditions.Conditions = append(negatedConditions.Conditions, negatedCond)
		}
		def.UserConditions = negatedConditions
	}

	if criterion.EventCondition != nil {
		// Negate event condition by inverting the count check
		negatedEvent := &v1.EventCondition{
			EventName:      criterion.EventCondition.EventName,
			LookbackDays:   criterion.EventCondition.LookbackDays,
			PropertyFilter: criterion.EventCondition.PropertyFilter,
		}
		// If original is "count >= N", negated is "count < N" (which is count == 0 for GTE 1)
		if criterion.EventCondition.CountOperator == v1.ComparisonOperator_COMPARISON_OPERATOR_GTE &&
			criterion.EventCondition.CountValue == 1 {
			negatedEvent.CountOperator = v1.ComparisonOperator_COMPARISON_OPERATOR_EQ
			negatedEvent.CountValue = 0
		} else {
			negatedEvent.CountOperator = v1.ComparisonOperator_COMPARISON_OPERATOR_LT
			negatedEvent.CountValue = criterion.EventCondition.CountValue
		}
		def.EventConditions = append(def.EventConditions, negatedEvent)
	}

	return def
}

// BuildComposite creates a composite segment from child segment IDs
func (b *SegmentBuilder) BuildComposite(childSegmentIDs []string, logic v1.LogicalOperator) *v1.SegmentDefinition {
	childRefs := make([]*v1.ChildSegmentRef, len(childSegmentIDs))
	for i, id := range childSegmentIDs {
		childRefs[i] = &v1.ChildSegmentRef{SegmentId: id}
	}
	return &v1.SegmentDefinition{
		Type:          v1.SegmentType_SEGMENT_TYPE_COMPOSITE,
		ChildSegments: childRefs,
		ChildLogic:    logic,
	}
}

// ========== Criteria Templates ==========

// CriteriaTemplate describes an available criterion type
type CriteriaTemplate struct {
	Name        string
	Description string
	Type        string
	Params      []ParamDefinition
}

// ParamDefinition describes a parameter for a criterion
type ParamDefinition struct {
	Name        string
	Type        string
	Required    bool
	Default     string
	Description string
}

// GetCriteriaTemplates returns all available criteria templates
func (c *CriteriaLibrary) GetCriteriaTemplates() map[string]*CriteriaTemplate {
	return map[string]*CriteriaTemplate{
		"A7": {
			Name:        "Active 7 Days",
			Description: "Users who were active in the last 7 days",
			Type:        "A7",
			Params:      nil,
		},
		"A30": {
			Name:        "Active 30 Days",
			Description: "Users who were active in the last 30 days",
			Type:        "A30",
			Params:      nil,
		},
		"ACTIVE_N_DAYS": {
			Name:        "Active N Days",
			Description: "Users who were active in the last N days",
			Type:        "ACTIVE_N_DAYS",
			Params: []ParamDefinition{
				{Name: "days", Type: "int", Required: true, Description: "Number of days to look back"},
			},
		},
		"PU": {
			Name:        "Paying Users",
			Description: "Users who have made at least one purchase",
			Type:        "PU",
			Params:      nil,
		},
		"NPU": {
			Name:        "Non-Paying Users",
			Description: "Users who have never made a purchase",
			Type:        "NPU",
			Params:      nil,
		},
		"HIGH_VALUE": {
			Name:        "High Value Users",
			Description: "Users with total revenue above a threshold",
			Type:        "HIGH_VALUE",
			Params: []ParamDefinition{
				{Name: "threshold", Type: "float", Default: "100", Description: "Revenue threshold"},
			},
		},
		"NEW_USERS": {
			Name:        "New Users",
			Description: "Users who joined recently",
			Type:        "NEW_USERS",
			Params: []ParamDefinition{
				{Name: "days", Type: "int", Default: "7", Description: "Number of days to consider as 'new'"},
			},
		},
		"CHURNED": {
			Name:        "Churned Users",
			Description: "Users who were previously active but have not returned",
			Type:        "CHURNED",
			Params: []ParamDefinition{
				{Name: "days", Type: "int", Default: "30", Description: "Days of inactivity to consider churned"},
			},
		},
		"PLATFORM": {
			Name:        "Platform Users",
			Description: "Users on a specific platform",
			Type:        "PLATFORM",
			Params: []ParamDefinition{
				{Name: "platform", Type: "string", Required: true, Description: "Platform name (web_mobile, web_pc, app)"},
			},
		},
		"COUNTRY": {
			Name:        "Country Users",
			Description: "Users from a specific country",
			Type:        "COUNTRY",
			Params: []ParamDefinition{
				{Name: "country", Type: "string", Required: true, Description: "Country code (e.g., VN)"},
			},
		},
		"EVENT_PERFORMED": {
			Name:        "Event Performed",
			Description: "Users who performed a specific event",
			Type:        "EVENT_PERFORMED",
			Params: []ParamDefinition{
				{Name: "event_name", Type: "string", Required: true, Description: "Event name"},
				{Name: "days", Type: "int", Default: "30", Description: "Lookback days"},
				{Name: "min_count", Type: "int", Default: "1", Description: "Minimum event count"},
			},
		},
		"EVENT_NOT_PERFORMED": {
			Name:        "Event Not Performed",
			Description: "Users who did NOT perform a specific event",
			Type:        "EVENT_NOT_PERFORMED",
			Params: []ParamDefinition{
				{Name: "event_name", Type: "string", Required: true, Description: "Event name"},
				{Name: "days", Type: "int", Default: "30", Description: "Lookback days"},
			},
		},
		"VIP_LEVEL": {
			Name:        "VIP Level",
			Description: "Users at a specific VIP level (per app)",
			Type:        "VIP_LEVEL",
			Params: []ParamDefinition{
				{Name: "app_id", Type: "string", Required: true, Description: "App/Game ID"},
				{Name: "level", Type: "int", Required: true, Description: "VIP level"},
			},
		},
		"VIP_LEVEL_GTE": {
			Name:        "VIP Level At Least",
			Description: "Users at or above a specific VIP level (per app)",
			Type:        "VIP_LEVEL_GTE",
			Params: []ParamDefinition{
				{Name: "app_id", Type: "string", Required: true, Description: "App/Game ID"},
				{Name: "min_level", Type: "int", Required: true, Description: "Minimum VIP level"},
			},
		},
		"VIP_LEVEL_RANGE": {
			Name:        "VIP Level Range",
			Description: "Users within a VIP level range (per app)",
			Type:        "VIP_LEVEL_RANGE",
			Params: []ParamDefinition{
				{Name: "app_id", Type: "string", Required: true, Description: "App/Game ID"},
				{Name: "min_level", Type: "int", Required: true, Description: "Minimum VIP level"},
				{Name: "max_level", Type: "int", Required: true, Description: "Maximum VIP level"},
			},
		},
	}
}
