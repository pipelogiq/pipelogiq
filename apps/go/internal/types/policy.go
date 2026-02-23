package types

import "time"

type PolicyType string

const (
	PolicyTypeRateLimit      PolicyType = "rate_limit"
	PolicyTypeRetry          PolicyType = "retry"
	PolicyTypeTimeout        PolicyType = "timeout"
	PolicyTypeCircuitBreaker PolicyType = "circuit_breaker"
)

type PolicyStatus string

const (
	PolicyStatusActive   PolicyStatus = "active"
	PolicyStatusPaused   PolicyStatus = "paused"
	PolicyStatusDisabled PolicyStatus = "disabled"
)

type PolicyEnvironment string

const (
	PolicyEnvironmentProd    PolicyEnvironment = "prod"
	PolicyEnvironmentStaging PolicyEnvironment = "staging"
	PolicyEnvironmentDev     PolicyEnvironment = "dev"
	PolicyEnvironmentAll     PolicyEnvironment = "all"
)

type Policy struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	Type        PolicyType        `json:"type"`
	Status      PolicyStatus      `json:"status"`
	Environment PolicyEnvironment `json:"environment"`
	Targeting   PolicyTargeting   `json:"targeting"`
	Rule        PolicyRule        `json:"rule"`
	CreatedAt   time.Time         `json:"createdAt"`
	CreatedBy   string            `json:"createdBy"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	UpdatedBy   string            `json:"updatedBy"`
	Version     int               `json:"version"`
}

type PolicyTargeting struct {
	Pipelines   []string `json:"pipelines"`
	Stages      []string `json:"stages"`
	Handlers    []string `json:"handlers"`
	TagsInclude []string `json:"tagsInclude"`
	TagsExclude []string `json:"tagsExclude"`
}

type RetryOnRule struct {
	HTTPStatus []int    `json:"httpStatus,omitempty"`
	ErrorCodes []string `json:"errorCodes,omitempty"`
}

type PolicyRule struct {
	Limit            *int         `json:"limit,omitempty"`
	WindowSeconds    *int         `json:"windowSeconds,omitempty"`
	KeyBy            *string      `json:"keyBy,omitempty"`
	Burst            *int         `json:"burst,omitempty"`
	MaxAttempts      *int         `json:"maxAttempts,omitempty"`
	Backoff          *string      `json:"backoff,omitempty"`
	BaseDelayMs      *int         `json:"baseDelayMs,omitempty"`
	MaxDelayMs       *int         `json:"maxDelayMs,omitempty"`
	Jitter           *bool        `json:"jitter,omitempty"`
	RetryOn          *RetryOnRule `json:"retryOn,omitempty"`
	TimeoutMs        *int         `json:"timeoutMs,omitempty"`
	AppliesTo        *string      `json:"appliesTo,omitempty"`
	FailureThreshold *int         `json:"failureThreshold,omitempty"`
	OpenSeconds      *int         `json:"openSeconds,omitempty"`
	HalfOpenMaxCalls *int         `json:"halfOpenMaxCalls,omitempty"`
}

type PolicyEventType string

const (
	PolicyEventTypeCreated   PolicyEventType = "created"
	PolicyEventTypeUpdated   PolicyEventType = "updated"
	PolicyEventTypeEnabled   PolicyEventType = "enabled"
	PolicyEventTypeDisabled  PolicyEventType = "disabled"
	PolicyEventTypePaused    PolicyEventType = "paused"
	PolicyEventTypeResumed   PolicyEventType = "resumed"
	PolicyEventTypeDeleted   PolicyEventType = "deleted"
	PolicyEventTypeTriggered PolicyEventType = "triggered"
)

type PolicyEvent struct {
	ID       string          `json:"id"`
	PolicyID string          `json:"policyId"`
	TS       time.Time       `json:"ts"`
	Actor    string          `json:"actor"`
	Type     PolicyEventType `json:"type"`
	Details  map[string]any  `json:"details,omitempty"`
}

type PolicyListItem struct {
	Policy
	LastTriggeredAt     *time.Time `json:"lastTriggeredAt,omitempty"`
	TriggerCountInRange int        `json:"triggerCountInRange"`
}

type PolicyListResponse struct {
	Items      []PolicyListItem `json:"items"`
	TotalCount int              `json:"totalCount"`
}

type PolicyInsightsTopPolicy struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Triggers int    `json:"triggers"`
}

type PolicyInsightsResponse struct {
	ActivePoliciesCount     int                      `json:"activePoliciesCount"`
	PoliciesTriggered       int                      `json:"policiesTriggered"`
	ActionsBlockedThrottled int                      `json:"actionsBlockedThrottled"`
	TopPolicy               *PolicyInsightsTopPolicy `json:"topPolicy,omitempty"`
}

type PolicyAuditResponse struct {
	PolicyID string        `json:"policyId"`
	Events   []PolicyEvent `json:"events"`
}

type PolicyTargetOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PolicyTargetOptionsResponse struct {
	Environments []PolicyEnvironment  `json:"environments"`
	Pipelines    []PolicyTargetOption `json:"pipelines"`
	Stages       []string             `json:"stages"`
	Handlers     []string             `json:"handlers"`
	Tags         []string             `json:"tags"`
}

type PolicyPreviewRequest struct {
	Environment PolicyEnvironment `json:"environment"`
	Targeting   PolicyTargeting   `json:"targeting"`
}

type PolicyPreviewResponse struct {
	Pipelines int `json:"pipelines"`
	Stages    int `json:"stages"`
	Handlers  int `json:"handlers"`
}
