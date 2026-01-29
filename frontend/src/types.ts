// Enums matching backend
export const SegmentType = { STATIC: 1, DYNAMIC: 2, COMPOSITE: 3 } as const;
export const Operator = { AND: 1, OR: 2 } as const;
export const Comparison = {
  EQ: 1, NEQ: 2, GT: 3, GTE: 4, LT: 5, LTE: 6, IN: 7, NOT_IN: 8, BETWEEN: 9, CONTAINS: 10
} as const;

export interface ConditionValue {
  stringValue?: string;
  intValue?: number;
  doubleValue?: number;
  boolValue?: boolean;
  stringList?: { values: string[] };
}

export interface Condition {
  field: string;
  operator: number;
  value: ConditionValue;
}

export interface ConditionGroup {
  operator: number;
  conditions: Condition[];
  groups?: ConditionGroup[];
}

export interface ChildSegmentRef {
  segmentId: string;
  negated?: boolean;
}

export interface SegmentDefinition {
  type: number;
  userConditions?: ConditionGroup;
  eventConditions?: EventCondition[];
  overallLogic?: number;
  childSegments?: ChildSegmentRef[];
  childLogic?: number;
}

export interface EventCondition {
  eventName: string;
  lookbackDays: number;
  countOperator: number;
  countValue: number;
}

export interface Segment {
  id?: string;
  name: string;
  description?: string;
  definition: SegmentDefinition;
}

// Criteria catalog
export interface CriteriaItem {
  id: string;
  label: string;
  field: string;
  category: 'activity' | 'payment' | 'churned' | 'profile' | 'revenue';
  defaultOperator: number;
  valueType: 'bool' | 'string' | 'number' | 'list';
}

export const CRITERIA_CATALOG: CriteriaItem[] = [
  // Activity
  { id: 'a1', label: 'A1 (1 day)', field: 'is_active_1d', category: 'activity', defaultOperator: 1, valueType: 'bool' },
  { id: 'a3', label: 'A3 (3 days)', field: 'is_active_3d', category: 'activity', defaultOperator: 1, valueType: 'bool' },
  { id: 'a7', label: 'A7 (7 days)', field: 'is_active_7d', category: 'activity', defaultOperator: 1, valueType: 'bool' },
  { id: 'a30', label: 'A30 (30 days)', field: 'is_active_30d', category: 'activity', defaultOperator: 1, valueType: 'bool' },
  { id: 'a90', label: 'A90 (90 days)', field: 'is_active_90d', category: 'activity', defaultOperator: 1, valueType: 'bool' },
  // Payment
  { id: 'pu1', label: 'PU1 (1 day)', field: 'is_pu_1d', category: 'payment', defaultOperator: 1, valueType: 'bool' },
  { id: 'pu3', label: 'PU3 (3 days)', field: 'is_pu_3d', category: 'payment', defaultOperator: 1, valueType: 'bool' },
  { id: 'pu7', label: 'PU7 (7 days)', field: 'is_pu_7d', category: 'payment', defaultOperator: 1, valueType: 'bool' },
  { id: 'pu30', label: 'PU30 (30 days)', field: 'is_pu_30d', category: 'payment', defaultOperator: 1, valueType: 'bool' },
  { id: 'pu90', label: 'PU90 (90 days)', field: 'is_pu_90d', category: 'payment', defaultOperator: 1, valueType: 'bool' },
  // Churned
  { id: 'ch1', label: 'Churned 1d', field: 'is_churned_1d', category: 'churned', defaultOperator: 1, valueType: 'bool' },
  { id: 'ch3', label: 'Churned 3d', field: 'is_churned_3d', category: 'churned', defaultOperator: 1, valueType: 'bool' },
  { id: 'ch7', label: 'Churned 7d', field: 'is_churned_7d', category: 'churned', defaultOperator: 1, valueType: 'bool' },
  { id: 'ch30', label: 'Churned 30d', field: 'is_churned_30d', category: 'churned', defaultOperator: 1, valueType: 'bool' },
  { id: 'ch90', label: 'Churned 90d', field: 'is_churned_90d', category: 'churned', defaultOperator: 1, valueType: 'bool' },
  // Profile
  { id: 'platform', label: 'Platform', field: 'platform', category: 'profile', defaultOperator: 7, valueType: 'list' },
  { id: 'country', label: 'Country', field: 'country', category: 'profile', defaultOperator: 7, valueType: 'list' },
  { id: 'os', label: 'OS', field: 'os', category: 'profile', defaultOperator: 7, valueType: 'list' },
  { id: 'language', label: 'Language', field: 'language', category: 'profile', defaultOperator: 7, valueType: 'list' },
  // Revenue
  { id: 'revenue', label: 'Total Revenue', field: 'total_revenue', category: 'revenue', defaultOperator: 4, valueType: 'number' },
  { id: 'purchases', label: 'Total Purchases', field: 'total_purchases', category: 'revenue', defaultOperator: 4, valueType: 'number' },
];

export const CATEGORY_COLORS: Record<string, string> = {
  activity: '#22c55e',
  payment: '#3b82f6',
  churned: '#ef4444',
  profile: '#8b5cf6',
  revenue: '#f59e0b',
};
