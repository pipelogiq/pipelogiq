package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

type StagePolicyRuntime interface {
	RuntimePolicies(ctx context.Context) ([]types.Policy, error)
	RecordRuntimeEvaluation(ctx context.Context, scope types.PolicyRuntimeScope, evaluation types.PolicyRuntimeEvaluation, source string) error
}

func (s *Store) SetStagePolicyRuntime(runtime StagePolicyRuntime) {
	s.policyRuntime = runtime
}

func (s *Store) ResolveStagePolicies(ctx context.Context, stageID int) (types.PolicyEffectiveResolutionResponse, error) {
	scope, evaluation, err := s.resolveStagePolicies(ctx, stageID)
	if err != nil {
		return types.PolicyEffectiveResolutionResponse{}, err
	}

	return types.PolicyEffectiveResolutionResponse{
		Scope:                   scope,
		PolicyRuntimeEvaluation: evaluation,
	}, nil
}

func (s *Store) resolveStagePolicies(ctx context.Context, stageID int) (types.PolicyRuntimeScope, types.PolicyRuntimeEvaluation, error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return types.PolicyRuntimeScope{}, types.PolicyRuntimeEvaluation{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	scope, err := s.loadPolicyRuntimeScopeTx(ctx, tx, stageID)
	if err != nil {
		return types.PolicyRuntimeScope{}, types.PolicyRuntimeEvaluation{}, err
	}

	evaluation, err := s.evaluateStagePoliciesTx(ctx, tx, scope)
	if err != nil {
		return types.PolicyRuntimeScope{}, types.PolicyRuntimeEvaluation{}, err
	}
	return scope, evaluation, nil
}

func (s *Store) evaluateStagePoliciesTx(ctx context.Context, tx *sqlx.Tx, scope types.PolicyRuntimeScope) (types.PolicyRuntimeEvaluation, error) {
	evaluation := types.PolicyRuntimeEvaluation{
		Action: types.PolicyRuntimeActionAllow,
	}
	if s.policyRuntime == nil {
		return evaluation, nil
	}

	policies, err := s.policyRuntime.RuntimePolicies(ctx)
	if err != nil {
		return types.PolicyRuntimeEvaluation{}, err
	}

	matched := make([]types.Policy, 0, len(policies))
	for _, policy := range policies {
		if policy.Status != types.PolicyStatusActive {
			continue
		}
		if !policyMatchesRuntimeScope(policy, scope) {
			continue
		}
		matched = append(matched, policy)
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Type != matched[j].Type {
			return matched[i].Type < matched[j].Type
		}
		leftSpecificity := policySpecificityScore(matched[i])
		rightSpecificity := policySpecificityScore(matched[j])
		if leftSpecificity != rightSpecificity {
			return leftSpecificity > rightSpecificity
		}
		if matched[i].Source != matched[j].Source {
			return policySourceRank(matched[i].Source) < policySourceRank(matched[j].Source)
		}
		return strings.ToLower(matched[i].Name) < strings.ToLower(matched[j].Name)
	})

	if len(matched) == 0 {
		return evaluation, nil
	}

	evaluation.MatchedPolicies = make([]types.PolicyRuntimeMatchedPolicy, 0, len(matched))
	matchedIndexByID := make(map[string]int, len(matched))
	for _, policy := range matched {
		matchedPolicy := types.PolicyRuntimeMatchedPolicy{
			PolicyID:         policy.ID,
			Name:             policy.Name,
			Source:           policy.Source,
			Type:             policy.Type,
			Action:           types.PolicyRuntimeActionAllow,
			SpecificityScore: policySpecificityScore(policy),
			Outcome:          "candidate",
			Explanation:      describePolicyMatch(policy, scope),
		}
		evaluation.MatchedPolicies = append(evaluation.MatchedPolicies, matchedPolicy)
		matchedIndexByID[policy.ID] = len(evaluation.MatchedPolicies) - 1
	}
	for i := range evaluation.MatchedPolicies {
		evaluation.MatchedPolicies[i].Precedence = i + 1
	}

	evaluation.ResolvedRules = resolvePolicyRuntimeRules(scope, matched, &evaluation, matchedIndexByID)

	for _, policy := range matched {
		if policy.Type != types.PolicyTypeCircuitBreaker {
			continue
		}
		blocked, reason, blockUntil, err := s.evaluateCircuitBreakerTx(ctx, tx, scope, policy)
		if err != nil {
			return types.PolicyRuntimeEvaluation{}, err
		}
		if !blocked {
			continue
		}
		evaluation.Action = types.PolicyRuntimeActionBlock
		evaluation.Reason = reason
		evaluation.Explain = append(evaluation.Explain, reason)
		matchedIdx := matchedIndexByID[policy.ID]
		evaluation.MatchedPolicies[matchedIdx].Action = types.PolicyRuntimeActionBlock
		evaluation.MatchedPolicies[matchedIdx].Reason = reason
		evaluation.MatchedPolicies[matchedIdx].ThrottleUntil = blockUntil
		evaluation.MatchedPolicies[matchedIdx].Outcome = "triggered"
		evaluation.MatchedPolicies[matchedIdx].Explanation = fmt.Sprintf("%s Runtime blocked dispatch because the breaker is still open.", evaluation.MatchedPolicies[matchedIdx].Explanation)
		return evaluation, nil
	}

	for _, policy := range matched {
		if policy.Type != types.PolicyTypeRateLimit {
			continue
		}
		throttled, reason, throttleUntil, err := s.evaluateRateLimitTx(ctx, tx, scope, policy)
		if err != nil {
			return types.PolicyRuntimeEvaluation{}, err
		}
		if !throttled {
			continue
		}
		evaluation.Action = types.PolicyRuntimeActionThrottle
		evaluation.Reason = reason
		evaluation.ThrottleUntil = throttleUntil
		evaluation.Explain = append(evaluation.Explain, reason)
		matchedIdx := matchedIndexByID[policy.ID]
		evaluation.MatchedPolicies[matchedIdx].Action = types.PolicyRuntimeActionThrottle
		evaluation.MatchedPolicies[matchedIdx].Reason = reason
		evaluation.MatchedPolicies[matchedIdx].ThrottleUntil = throttleUntil
		evaluation.MatchedPolicies[matchedIdx].Outcome = "triggered"
		evaluation.MatchedPolicies[matchedIdx].Explanation = fmt.Sprintf("%s Runtime throttled dispatch because the current window is saturated.", evaluation.MatchedPolicies[matchedIdx].Explanation)
		return evaluation, nil
	}

	return evaluation, nil
}

func resolvePolicyRuntimeRules(
	scope types.PolicyRuntimeScope,
	matched []types.Policy,
	evaluation *types.PolicyRuntimeEvaluation,
	matchedIndexByID map[string]int,
) []types.PolicyRuntimeResolvedRule {
	resolved := make([]types.PolicyRuntimeResolvedRule, 0, 4)

	if rule := resolveTimeoutRule(scope, matched, evaluation, matchedIndexByID); rule != nil {
		resolved = append(resolved, *rule)
	}
	if rule := resolveRetryRule(matched, evaluation, matchedIndexByID); rule != nil {
		resolved = append(resolved, *rule)
	}
	if rule := resolveStackedRule(types.PolicyTypeRateLimit, "stacked_any_throttle", "All matched rate-limit policies remain active; runtime throttles when any one is exceeded.", matched, evaluation, matchedIndexByID); rule != nil {
		resolved = append(resolved, *rule)
	}
	if rule := resolveStackedRule(types.PolicyTypeCircuitBreaker, "stacked_any_block", "All matched circuit breakers remain active; runtime blocks when any breaker is open.", matched, evaluation, matchedIndexByID); rule != nil {
		resolved = append(resolved, *rule)
	}

	return resolved
}

func resolveTimeoutRule(
	scope types.PolicyRuntimeScope,
	matched []types.Policy,
	evaluation *types.PolicyRuntimeEvaluation,
	matchedIndexByID map[string]int,
) *types.PolicyRuntimeResolvedRule {
	baseTimeoutMs := 0
	if scope.BaseTimeoutSeconds != nil && *scope.BaseTimeoutSeconds > 0 {
		baseTimeoutMs = *scope.BaseTimeoutSeconds * 1000
	}

	timeoutPolicies := make([]types.Policy, 0)
	policyIDs := make([]string, 0)
	for _, policy := range matched {
		if policy.Type != types.PolicyTypeTimeout {
			continue
		}
		timeoutPolicies = append(timeoutPolicies, policy)
		policyIDs = append(policyIDs, policy.ID)
	}
	if len(timeoutPolicies) == 0 && baseTimeoutMs == 0 {
		return nil
	}

	var winner *types.Policy
	var effectiveTimeoutMs *int
	if baseTimeoutMs > 0 {
		effectiveTimeoutMs = cloneInt(&baseTimeoutMs)
	}
	for _, policy := range timeoutPolicies {
		idx := matchedIndexByID[policy.ID]
		appliesTo := ""
		if policy.Rule.AppliesTo != nil {
			appliesTo = strings.TrimSpace(strings.ToLower(*policy.Rule.AppliesTo))
		}
		if appliesTo != "" && appliesTo != "step" {
			evaluation.MatchedPolicies[idx].Outcome = "ignored"
			evaluation.MatchedPolicies[idx].Explanation = fmt.Sprintf("%s Ignored for stage timeout because appliesTo=%q.", evaluation.MatchedPolicies[idx].Explanation, appliesTo)
			continue
		}
		if policy.Rule.TimeoutMs == nil || *policy.Rule.TimeoutMs <= 0 {
			evaluation.MatchedPolicies[idx].Outcome = "ignored"
			evaluation.MatchedPolicies[idx].Explanation = fmt.Sprintf("%s Ignored because timeoutMs is not set.", evaluation.MatchedPolicies[idx].Explanation)
			continue
		}
		if effectiveTimeoutMs == nil || *policy.Rule.TimeoutMs < *effectiveTimeoutMs {
			value := *policy.Rule.TimeoutMs
			effectiveTimeoutMs = &value
			candidate := policy
			winner = &candidate
		}
	}
	if effectiveTimeoutMs == nil {
		if baseTimeoutMs == 0 {
			return nil
		}
		evaluation.EffectiveTimeoutMs = cloneInt(&baseTimeoutMs)
		evaluation.Explain = append(evaluation.Explain, fmt.Sprintf("Effective timeout resolved to %dms from the stage base timeout because no matching step timeout policy overrode it.", baseTimeoutMs))
		return &types.PolicyRuntimeResolvedRule{
			Type:               types.PolicyTypeTimeout,
			Strategy:           "minimum_timeout",
			PolicyIDs:          policyIDs,
			EffectiveTimeoutMs: cloneInt(&baseTimeoutMs),
			Explanation:        fmt.Sprintf("Stage base timeout of %dms remains effective because no matching step timeout policy overrode it.", baseTimeoutMs),
		}
	}

	evaluation.EffectiveTimeoutMs = cloneInt(effectiveTimeoutMs)
	for _, policy := range timeoutPolicies {
		idx := matchedIndexByID[policy.ID]
		evaluation.MatchedPolicies[idx].EffectiveTimeoutMs = cloneInt(effectiveTimeoutMs)
		if evaluation.MatchedPolicies[idx].Outcome == "ignored" {
			continue
		}
		if winner != nil && policy.ID == winner.ID {
			evaluation.MatchedPolicies[idx].Outcome = "selected"
			evaluation.MatchedPolicies[idx].Explanation = fmt.Sprintf("%s Selected because it produces the lowest effective step timeout (%dms).", evaluation.MatchedPolicies[idx].Explanation, *effectiveTimeoutMs)
			continue
		}
		evaluation.MatchedPolicies[idx].Outcome = "shadowed"
		if winner != nil {
			evaluation.MatchedPolicies[idx].OverriddenByPolicy = cloneString(winner.ID)
			evaluation.MatchedPolicies[idx].Explanation = fmt.Sprintf("%s Shadowed because %q produces a stricter effective timeout of %dms.", evaluation.MatchedPolicies[idx].Explanation, winner.Name, *effectiveTimeoutMs)
		}
	}

	explanation := fmt.Sprintf("Effective timeout resolved to %dms using minimum_timeout semantics across stage base timeout and matched timeout policies.", *effectiveTimeoutMs)
	if winner != nil {
		explanation = fmt.Sprintf("Effective timeout resolved to %dms; policy %q won because it is the strictest matching step timeout.", *effectiveTimeoutMs, winner.Name)
	} else if baseTimeoutMs > 0 {
		explanation = fmt.Sprintf("Effective timeout resolved to %dms from the stage base timeout; matching timeout policies did not produce a stricter step timeout.", *effectiveTimeoutMs)
	}
	evaluation.Explain = append(evaluation.Explain, explanation)

	var winningPolicyID *string
	if winner != nil {
		winningPolicyID = cloneString(winner.ID)
	}
	return &types.PolicyRuntimeResolvedRule{
		Type:               types.PolicyTypeTimeout,
		Strategy:           "minimum_timeout",
		PolicyIDs:          policyIDs,
		WinningPolicyID:    winningPolicyID,
		EffectiveTimeoutMs: cloneInt(effectiveTimeoutMs),
		Explanation:        explanation,
	}
}

func resolveRetryRule(
	matched []types.Policy,
	evaluation *types.PolicyRuntimeEvaluation,
	matchedIndexByID map[string]int,
) *types.PolicyRuntimeResolvedRule {
	retryPolicies := make([]types.Policy, 0)
	for _, policy := range matched {
		if policy.Type == types.PolicyTypeRetry {
			retryPolicies = append(retryPolicies, policy)
		}
	}
	if len(retryPolicies) == 0 {
		return nil
	}

	winner := retryPolicies[0]
	policyIDs := make([]string, 0, len(retryPolicies))
	for _, policy := range retryPolicies {
		policyIDs = append(policyIDs, policy.ID)
		idx := matchedIndexByID[policy.ID]
		if policy.ID == winner.ID {
			evaluation.MatchedPolicies[idx].Outcome = "selected"
			evaluation.MatchedPolicies[idx].Explanation = fmt.Sprintf("%s Selected as the effective retry policy because it has the highest precedence among matching retry policies.", evaluation.MatchedPolicies[idx].Explanation)
			continue
		}
		evaluation.MatchedPolicies[idx].Outcome = "shadowed"
		evaluation.MatchedPolicies[idx].OverriddenByPolicy = cloneString(winner.ID)
		evaluation.MatchedPolicies[idx].Explanation = fmt.Sprintf("%s Shadowed by higher-precedence retry policy %q.", evaluation.MatchedPolicies[idx].Explanation, winner.Name)
	}

	explanation := fmt.Sprintf("Retry policy resolves by precedence; %q is currently effective.", winner.Name)
	evaluation.Explain = append(evaluation.Explain, explanation)
	return &types.PolicyRuntimeResolvedRule{
		Type:            types.PolicyTypeRetry,
		Strategy:        "highest_precedence",
		PolicyIDs:       policyIDs,
		WinningPolicyID: cloneString(winner.ID),
		Explanation:     explanation,
	}
}

func resolveStackedRule(
	policyType types.PolicyType,
	strategy string,
	explanation string,
	matched []types.Policy,
	evaluation *types.PolicyRuntimeEvaluation,
	matchedIndexByID map[string]int,
) *types.PolicyRuntimeResolvedRule {
	policiesByType := make([]types.Policy, 0)
	for _, policy := range matched {
		if policy.Type == policyType {
			policiesByType = append(policiesByType, policy)
		}
	}
	if len(policiesByType) == 0 {
		return nil
	}

	policyIDs := make([]string, 0, len(policiesByType))
	for _, policy := range policiesByType {
		policyIDs = append(policyIDs, policy.ID)
		idx := matchedIndexByID[policy.ID]
		if evaluation.MatchedPolicies[idx].Outcome == "" || evaluation.MatchedPolicies[idx].Outcome == "candidate" {
			evaluation.MatchedPolicies[idx].Outcome = "merged"
		}
		evaluation.MatchedPolicies[idx].Explanation = fmt.Sprintf("%s %s", evaluation.MatchedPolicies[idx].Explanation, explanation)
	}

	evaluation.Explain = append(evaluation.Explain, explanation)
	return &types.PolicyRuntimeResolvedRule{
		Type:        policyType,
		Strategy:    strategy,
		PolicyIDs:   policyIDs,
		Explanation: explanation,
	}
}

func (s *Store) loadPolicyRuntimeScopeTx(ctx context.Context, tx *sqlx.Tx, stageID int) (types.PolicyRuntimeScope, error) {
	var row struct {
		StageID          int            `db:"stage_id"`
		PipelineID       int            `db:"pipeline_id"`
		ApplicationID    sql.NullInt64  `db:"application_id"`
		StageName        string         `db:"stage_name"`
		StageHandlerName string         `db:"stage_handler_name"`
		Environment      sql.NullString `db:"environment"`
		TimeoutSeconds   sql.NullInt64  `db:"time_out"`
	}

	if err := tx.GetContext(ctx, &row, `
		SELECT
			s.id AS stage_id,
			s.pipeline_id,
			p.application_id,
			COALESCE(s.name, '') AS stage_name,
			COALESCE(s.stage_handler_name, '') AS stage_handler_name,
			COALESCE((
				SELECT LOWER(pci.value)
				FROM pipeline_context_item pci
				WHERE pci.pipeline_id = p.id
				  AND LOWER(pci.key) IN ('environment', 'env')
				ORDER BY pci.id DESC
				LIMIT 1
			), '') AS environment,
			so.time_out
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		LEFT JOIN stage_options so ON so.stage_id = s.id
		WHERE s.id = $1
		ORDER BY so.id DESC
		LIMIT 1
	`, stageID); err != nil {
		return types.PolicyRuntimeScope{}, err
	}

	tags := []string{}
	tagRows := []string{}
	if err := tx.SelectContext(ctx, &tagRows, `
		SELECT COALESCE(k.value, '') AS value
		FROM pipeline_keyword pk
		JOIN keyword k ON k.id = pk.keyword_id
		WHERE pk.pipeline_id = $1
	`, row.PipelineID); err == nil {
		for _, tag := range tagRows {
			trimmed := strings.TrimSpace(tag)
			if trimmed != "" {
				tags = append(tags, trimmed)
			}
		}
	}

	scope := types.PolicyRuntimeScope{
		PipelineID:       row.PipelineID,
		StageID:          row.StageID,
		StageName:        row.StageName,
		StageHandlerName: row.StageHandlerName,
		Environment:      types.PolicyEnvironmentAll,
		Tags:             dedupeStrings(tags),
	}
	if row.ApplicationID.Valid {
		value := int(row.ApplicationID.Int64)
		scope.ApplicationID = &value
	}
	if env := strings.TrimSpace(row.Environment.String); env != "" {
		scope.Environment = types.PolicyEnvironment(env)
	}
	if row.TimeoutSeconds.Valid && row.TimeoutSeconds.Int64 > 0 {
		timeout := int(row.TimeoutSeconds.Int64)
		scope.BaseTimeoutSeconds = &timeout
	}
	return scope, nil
}

func effectiveTimeoutForScope(scope types.PolicyRuntimeScope, matched []types.Policy) *int {
	var effective *int
	if scope.BaseTimeoutSeconds != nil && *scope.BaseTimeoutSeconds > 0 {
		ms := *scope.BaseTimeoutSeconds * 1000
		effective = &ms
	}
	for _, policy := range matched {
		if policy.Type != types.PolicyTypeTimeout || policy.Rule.TimeoutMs == nil || *policy.Rule.TimeoutMs <= 0 {
			continue
		}
		if policy.Rule.AppliesTo != nil && strings.TrimSpace(*policy.Rule.AppliesTo) != "" && *policy.Rule.AppliesTo != "step" {
			continue
		}
		if effective == nil || *policy.Rule.TimeoutMs < *effective {
			value := *policy.Rule.TimeoutMs
			effective = &value
		}
	}
	return effective
}

func policySpecificityScore(policy types.Policy) int {
	score := 0
	score += len(policy.Targeting.Pipelines) * 8
	score += len(policy.Targeting.Stages) * 4
	score += len(policy.Targeting.Handlers) * 4
	score += len(policy.Targeting.TagsInclude) * 2
	score += len(policy.Targeting.TagsExclude) * 2
	if policy.Environment != "" && policy.Environment != types.PolicyEnvironmentAll {
		score++
	}
	return score
}

func policySourceRank(source types.PolicySource) int {
	if source == types.PolicySourceSystem {
		return 0
	}
	return 1
}

func describePolicyMatch(policy types.Policy, scope types.PolicyRuntimeScope) string {
	parts := make([]string, 0, 6)
	if policy.Environment != "" && policy.Environment != types.PolicyEnvironmentAll {
		parts = append(parts, fmt.Sprintf("env=%s", policy.Environment))
	}
	if len(policy.Targeting.Pipelines) > 0 {
		parts = append(parts, fmt.Sprintf("pipeline=%d", scope.PipelineID))
	}
	if len(policy.Targeting.Stages) > 0 {
		parts = append(parts, fmt.Sprintf("stage=%q", scope.StageName))
	}
	if len(policy.Targeting.Handlers) > 0 {
		parts = append(parts, fmt.Sprintf("handler=%q", scope.StageHandlerName))
	}
	if len(policy.Targeting.TagsInclude) > 0 {
		parts = append(parts, fmt.Sprintf("tags include %s", strings.Join(policy.Targeting.TagsInclude, ", ")))
	}
	if len(policy.Targeting.TagsExclude) > 0 {
		parts = append(parts, fmt.Sprintf("tags exclude %s", strings.Join(policy.Targeting.TagsExclude, ", ")))
	}
	if len(parts) == 0 {
		return "Matched as a global policy."
	}
	return fmt.Sprintf("Matched on %s.", strings.Join(parts, ", "))
}

func cloneString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	copied := value
	return &copied
}

func (s *Store) evaluateRateLimitTx(ctx context.Context, tx *sqlx.Tx, scope types.PolicyRuntimeScope, policy types.Policy) (bool, string, *time.Time, error) {
	if policy.Rule.Limit == nil || policy.Rule.WindowSeconds == nil || policy.Rule.KeyBy == nil {
		return false, "", nil, nil
	}

	windowStart := time.Now().UTC().Add(-time.Duration(*policy.Rule.WindowSeconds) * time.Second)
	keyBy := strings.ToLower(strings.TrimSpace(*policy.Rule.KeyBy))
	limit := *policy.Rule.Limit
	if policy.Rule.Burst != nil && *policy.Rule.Burst > 0 {
		limit += *policy.Rule.Burst
	}

	var count int
	var earliestRaw sql.NullString
	query := `
		SELECT COUNT(*), MIN(s.started_at)
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE s.id <> $1
		  AND s.started_at IS NOT NULL
		  AND s.started_at >= $2
		  AND COALESCE(s.is_event,false) = false
	`
	args := []any{scope.StageID, windowStart}
	switch keyBy {
	case "tenant":
		if scope.ApplicationID == nil {
			query += ` AND s.pipeline_id = $3`
			args = append(args, scope.PipelineID)
		} else {
			query += ` AND p.application_id = $3`
			args = append(args, *scope.ApplicationID)
		}
	case "user":
		query += ` AND s.pipeline_id = $3`
		args = append(args, scope.PipelineID)
	case "custom":
		query += ` AND LOWER(COALESCE(s.stage_handler_name, '')) = $3`
		args = append(args, strings.ToLower(scope.StageHandlerName))
	}

	if err := tx.QueryRowContext(ctx, query, args...).Scan(&count, &earliestRaw); err != nil {
		return false, "", nil, err
	}
	if count < limit {
		return false, "", nil, nil
	}

	throttleUntil := time.Now().UTC().Add(time.Duration(*policy.Rule.WindowSeconds) * time.Second)
	if earliest, ok := parseNullableDBTime(earliestRaw); ok {
		throttleUntil = earliest.UTC().Add(time.Duration(*policy.Rule.WindowSeconds) * time.Second)
	}
	reason := fmt.Sprintf("rate limit policy %q throttled dispatch", policy.Name)
	return true, reason, &throttleUntil, nil
}

func (s *Store) evaluateCircuitBreakerTx(ctx context.Context, tx *sqlx.Tx, scope types.PolicyRuntimeScope, policy types.Policy) (bool, string, *time.Time, error) {
	if policy.Rule.FailureThreshold == nil || policy.Rule.WindowSeconds == nil || policy.Rule.OpenSeconds == nil {
		return false, "", nil, nil
	}

	windowStart := time.Now().UTC().Add(-time.Duration(*policy.Rule.WindowSeconds) * time.Second)
	failureRows := []string{}
	if err := tx.SelectContext(ctx, &failureRows, `
		SELECT COALESCE(s.finished_at, s.started_at, s.created_at) AS ts
		FROM stage s
		WHERE s.id <> $1
		  AND s.status = $2
		  AND LOWER(COALESCE(s.stage_handler_name, '')) = $3
		  AND COALESCE(s.is_event,false) = false
	`, scope.StageID, types.StageStatusFailed, strings.ToLower(scope.StageHandlerName)); err != nil {
		return false, "", nil, err
	}

	failures := 0
	var latestFailure time.Time
	for _, raw := range failureRows {
		parsed, ok := parseNullableDBTime(sql.NullString{String: raw, Valid: strings.TrimSpace(raw) != ""})
		if !ok || parsed.Before(windowStart) {
			continue
		}
		failures++
		if latestFailure.IsZero() || parsed.After(latestFailure) {
			latestFailure = parsed
		}
	}
	if failures < *policy.Rule.FailureThreshold || latestFailure.IsZero() {
		return false, "", nil, nil
	}

	openUntil := latestFailure.UTC().Add(time.Duration(*policy.Rule.OpenSeconds) * time.Second)
	if !openUntil.After(time.Now().UTC()) {
		return false, "", nil, nil
	}
	reason := fmt.Sprintf("circuit breaker policy %q blocked dispatch", policy.Name)
	return true, reason, &openUntil, nil
}

func policyMatchesRuntimeScope(policy types.Policy, scope types.PolicyRuntimeScope) bool {
	if policy.Environment != "" && policy.Environment != types.PolicyEnvironmentAll && policy.Environment != scope.Environment {
		return false
	}

	if len(policy.Targeting.Pipelines) > 0 && !containsFold(policy.Targeting.Pipelines, strconv.Itoa(scope.PipelineID)) {
		return false
	}
	if len(policy.Targeting.Stages) > 0 && !containsFold(policy.Targeting.Stages, scope.StageName) {
		return false
	}
	if len(policy.Targeting.Handlers) > 0 && !containsFold(policy.Targeting.Handlers, scope.StageHandlerName) {
		return false
	}

	if len(policy.Targeting.TagsInclude) > 0 {
		for _, required := range policy.Targeting.TagsInclude {
			if !containsFold(scope.Tags, required) {
				return false
			}
		}
	}
	if len(policy.Targeting.TagsExclude) > 0 {
		for _, denied := range policy.Targeting.TagsExclude {
			if containsFold(scope.Tags, denied) {
				return false
			}
		}
	}

	return true
}

func containsFold(values []string, needle string) bool {
	needle = strings.TrimSpace(strings.ToLower(needle))
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(strings.ToLower(value)) == needle {
			return true
		}
	}
	return false
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// computeBackoffDelay returns the next retry delay for a given attempt number (1-based)
// based on the policy retry rule. Falls back to baseDelayMs=0 if rule fields are nil.
func computeBackoffDelay(rule types.PolicyRule, attempt int) time.Duration {
	baseMs := 0
	if rule.BaseDelayMs != nil && *rule.BaseDelayMs > 0 {
		baseMs = *rule.BaseDelayMs
	}
	if baseMs == 0 {
		return 0
	}

	maxMs := baseMs * 30 // sensible default cap
	if rule.MaxDelayMs != nil && *rule.MaxDelayMs > 0 {
		maxMs = *rule.MaxDelayMs
	}

	backoff := "fixed"
	if rule.Backoff != nil && *rule.Backoff != "" {
		backoff = strings.ToLower(strings.TrimSpace(*rule.Backoff))
	}

	if attempt < 1 {
		attempt = 1
	}

	baseMs64 := int64(baseMs)
	maxMs64 := int64(maxMs)
	var delayMs64 int64
	switch backoff {
	case "exponential":
		delayMs64 = baseMs64
		for i := 1; i < attempt; i++ {
			if delayMs64 >= maxMs64 || delayMs64 > maxMs64/2 {
				delayMs64 = maxMs64
				break
			}
			delayMs64 *= 2
		}
	case "linear":
		if int64(attempt) > maxMs64/baseMs64 {
			delayMs64 = maxMs64
		} else {
			delayMs64 = baseMs64 * int64(attempt)
		}
	default: // "fixed"
		delayMs64 = baseMs64
	}

	if delayMs64 > maxMs64 {
		delayMs64 = maxMs64
	}

	if rule.Jitter != nil && *rule.Jitter && delayMs64 > 0 {
		// ±10% jitter using time-based pseudo-randomness (no crypto needed here)
		jitterRange := delayMs64 / 10
		if jitterRange > 0 {
			// Use nanosecond timestamp as simple deterministic offset
			nanos := time.Now().UnixNano()
			sign := nanos % 2
			offset := (nanos >> 8) % jitterRange
			if sign == 0 {
				delayMs64 += offset
			} else {
				delayMs64 -= offset
				if delayMs64 < 0 {
					delayMs64 = 0
				}
			}
		}
	}
	if delayMs64 > maxMs64 {
		delayMs64 = maxMs64
	}

	return time.Duration(delayMs64) * time.Millisecond
}

// retryOnMatches reports whether an error code satisfies the RetryOn condition.
// If RetryOn is nil or has no ErrorCodes, it always matches (retry on any error).
func retryOnMatches(rule types.PolicyRule, errorCode string) bool {
	if rule.RetryOn == nil {
		return true
	}
	if len(rule.RetryOn.ErrorCodes) == 0 {
		return true
	}
	normalised := strings.ToLower(strings.TrimSpace(errorCode))
	for _, code := range rule.RetryOn.ErrorCodes {
		if strings.ToLower(strings.TrimSpace(code)) == normalised {
			return true
		}
	}
	return false
}

// effectiveRetryPolicy extracts the winning retry rule from a policy evaluation, if any.
func effectiveRetryPolicy(evaluation types.PolicyRuntimeEvaluation, allPolicies []types.Policy) *types.PolicyRule {
	for _, resolved := range evaluation.ResolvedRules {
		if resolved.Type != types.PolicyTypeRetry || resolved.WinningPolicyID == nil {
			continue
		}
		winnerID := *resolved.WinningPolicyID
		for _, p := range allPolicies {
			if p.ID == winnerID {
				rule := p.Rule
				return &rule
			}
		}
	}
	return nil
}

func parseNullableDBTime(value sql.NullString) (time.Time, bool) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05 -0700",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value.String); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
