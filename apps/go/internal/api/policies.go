package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pipelogiq/internal/types"
)

var errPolicyNotFound = errors.New("policy not found")

type upsertPolicyRequest struct {
	Name        string                  `json:"name"`
	Description *string                 `json:"description,omitempty"`
	Type        types.PolicyType        `json:"type"`
	Status      *types.PolicyStatus     `json:"status,omitempty"`
	Environment types.PolicyEnvironment `json:"environment"`
	Targeting   types.PolicyTargeting   `json:"targeting"`
	Rule        types.PolicyRule        `json:"rule"`
}

type policyDetailResponse struct {
	types.Policy
	LastTriggeredAt     *time.Time `json:"lastTriggeredAt,omitempty"`
	TriggerCountInRange int        `json:"triggerCountInRange"`
}

type policyStoreSnapshot struct {
	Policies []types.Policy      `json:"policies"`
	Events   []types.PolicyEvent `json:"events"`
}

type policyListFilter struct {
	Search     string
	Type       *types.PolicyType
	Status     *types.PolicyStatus
	Env        *types.PolicyEnvironment
	PipelineID string
	Range      time.Duration
	SortBy     string
	SortDir    string
}

type policyRepository struct {
	mu            sync.RWMutex
	policies      map[string]types.Policy
	events        map[string][]types.PolicyEvent
	filePath      string
	logger        *slog.Logger
	eventListener func(types.PolicyEvent)
}

// TODO: Swap this file-backed repository for DB tables once policy schema is live in production.
func newPolicyRepository(logger *slog.Logger) *policyRepository {
	repo := &policyRepository{
		policies: make(map[string]types.Policy),
		events:   make(map[string][]types.PolicyEvent),
		filePath: resolvePolicyStorePath(),
		logger:   logger,
	}

	if err := repo.load(); err != nil {
		logger.Error("load policy store failed; starting with empty store", "err", err, "path", repo.filePath)
	}

	return repo
}

func (r *policyRepository) setEventListener(listener func(types.PolicyEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventListener = listener
}

func resolvePolicyStorePath() string {
	if envPath := strings.TrimSpace(os.Getenv("POLICY_STORE_PATH")); envPath != "" {
		return envPath
	}

	candidates := []string{
		"./data/policies.json",
		"../../data/policies.json",
	}

	for _, candidate := range candidates {
		if dirExists(filepath.Dir(candidate)) {
			return candidate
		}
	}

	return "./data/policies.json"
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func (r *policyRepository) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	content, err := os.ReadFile(r.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	if len(content) == 0 {
		return nil
	}

	var snapshot policyStoreSnapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return err
	}

	r.policies = make(map[string]types.Policy, len(snapshot.Policies))
	for _, policy := range snapshot.Policies {
		policy = normalizePolicy(policy)
		r.policies[policy.ID] = clonePolicy(policy)
	}

	r.events = make(map[string][]types.PolicyEvent)
	for _, event := range snapshot.Events {
		r.events[event.PolicyID] = append(r.events[event.PolicyID], clonePolicyEvent(event))
	}

	return nil
}

func (r *policyRepository) saveLocked() error {
	snapshot := policyStoreSnapshot{
		Policies: make([]types.Policy, 0, len(r.policies)),
		Events:   make([]types.PolicyEvent, 0, len(r.events)*2),
	}

	for _, policy := range r.policies {
		snapshot.Policies = append(snapshot.Policies, clonePolicy(policy))
	}
	sort.Slice(snapshot.Policies, func(i, j int) bool {
		return snapshot.Policies[i].UpdatedAt.After(snapshot.Policies[j].UpdatedAt)
	})

	for _, events := range r.events {
		for _, event := range events {
			snapshot.Events = append(snapshot.Events, clonePolicyEvent(event))
		}
	}
	sort.Slice(snapshot.Events, func(i, j int) bool {
		return snapshot.Events[i].TS.Before(snapshot.Events[j].TS)
	})

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(r.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tempFile := fmt.Sprintf("%s.tmp", r.filePath)
	if err := os.WriteFile(tempFile, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempFile, r.filePath)
}

func (r *policyRepository) list(filter policyListFilter) types.PolicyListResponse {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if filter.Range <= 0 {
		filter.Range = 24 * time.Hour
	}
	rangeStart := time.Now().UTC().Add(-filter.Range)

	items := make([]types.PolicyListItem, 0, len(r.policies))
	for _, policy := range r.policies {
		if !matchesPolicyFilter(policy, filter) {
			continue
		}

		lastTriggeredAt, triggerCount, _ := r.computeTriggerStatsLocked(policy.ID, rangeStart)
		items = append(items, types.PolicyListItem{
			Policy:              clonePolicy(policy),
			LastTriggeredAt:     lastTriggeredAt,
			TriggerCountInRange: triggerCount,
		})
	}

	sortPolicies(items, filter.SortBy, filter.SortDir)

	return types.PolicyListResponse{
		Items:      items,
		TotalCount: len(items),
	}
}

func (r *policyRepository) get(policyID string) (types.Policy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	policy, ok := r.policies[policyID]
	if !ok {
		return types.Policy{}, false
	}
	return clonePolicy(policy), true
}

func (r *policyRepository) exists(policyID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.policies[policyID]
	return ok
}

func (r *policyRepository) create(req upsertPolicyRequest, actor string) (types.Policy, error) {
	policy := types.Policy{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(req.Name),
		Description: normalizeDescription(req.Description),
		Type:        req.Type,
		Status:      types.PolicyStatusActive,
		Environment: req.Environment,
		Targeting:   normalizeTargeting(req.Targeting),
		Rule:        normalizePolicyRule(req.Rule),
		Version:     1,
	}

	if req.Status != nil {
		policy.Status = *req.Status
	}
	if policy.Environment == "" {
		policy.Environment = types.PolicyEnvironmentAll
	}

	now := time.Now().UTC()
	policy.CreatedAt = now
	policy.UpdatedAt = now
	policy.CreatedBy = actor
	policy.UpdatedBy = actor
	policy = normalizePolicy(policy)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.policies[policy.ID] = clonePolicy(policy)
	r.appendEventLocked(policy.ID, actor, types.PolicyEventTypeCreated, map[string]any{
		"version": policy.Version,
	})
	if err := r.saveLocked(); err != nil {
		r.logger.Error("save policy store failed", "err", err)
	}

	return clonePolicy(policy), nil
}

func (r *policyRepository) update(policyID string, req upsertPolicyRequest, actor string) (types.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.policies[policyID]
	if !ok {
		return types.Policy{}, errPolicyNotFound
	}

	previousVersion := existing.Version
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = normalizeDescription(req.Description)
	existing.Type = req.Type
	existing.Environment = req.Environment
	existing.Targeting = normalizeTargeting(req.Targeting)
	existing.Rule = normalizePolicyRule(req.Rule)
	if req.Status != nil {
		existing.Status = *req.Status
	}

	existing.Version++
	existing.UpdatedAt = time.Now().UTC()
	existing.UpdatedBy = actor
	existing = normalizePolicy(existing)

	r.policies[policyID] = clonePolicy(existing)
	r.appendEventLocked(policyID, actor, types.PolicyEventTypeUpdated, map[string]any{
		"fromVersion": previousVersion,
		"toVersion":   existing.Version,
	})
	if err := r.saveLocked(); err != nil {
		r.logger.Error("save policy store failed", "err", err)
	}

	return clonePolicy(existing), nil
}

func (r *policyRepository) setStatus(policyID string, status types.PolicyStatus, actor string, eventType types.PolicyEventType) (types.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	policy, ok := r.policies[policyID]
	if !ok {
		return types.Policy{}, errPolicyNotFound
	}

	if policy.Status == status {
		return clonePolicy(policy), nil
	}

	policy.Status = status
	policy.Version++
	policy.UpdatedAt = time.Now().UTC()
	policy.UpdatedBy = actor
	policy = normalizePolicy(policy)

	r.policies[policyID] = clonePolicy(policy)
	r.appendEventLocked(policyID, actor, eventType, map[string]any{
		"status":  status,
		"version": policy.Version,
	})
	if err := r.saveLocked(); err != nil {
		r.logger.Error("save policy store failed", "err", err)
	}

	return clonePolicy(policy), nil
}

func (r *policyRepository) duplicate(policyID, actor string) (types.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	source, ok := r.policies[policyID]
	if !ok {
		return types.Policy{}, errPolicyNotFound
	}

	now := time.Now().UTC()
	copyPolicy := clonePolicy(source)
	copyPolicy.ID = uuid.NewString()
	copyPolicy.Name = r.nextDuplicateNameLocked(source.Name)
	copyPolicy.Status = types.PolicyStatusDisabled
	copyPolicy.Version = 1
	copyPolicy.CreatedAt = now
	copyPolicy.UpdatedAt = now
	copyPolicy.CreatedBy = actor
	copyPolicy.UpdatedBy = actor

	r.policies[copyPolicy.ID] = clonePolicy(copyPolicy)
	r.appendEventLocked(copyPolicy.ID, actor, types.PolicyEventTypeCreated, map[string]any{
		"sourcePolicyId": policyID,
		"duplicated":     true,
		"version":        1,
	})
	if err := r.saveLocked(); err != nil {
		r.logger.Error("save policy store failed", "err", err)
	}

	return clonePolicy(copyPolicy), nil
}

func (r *policyRepository) delete(policyID, actor string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	policy, ok := r.policies[policyID]
	if !ok {
		return errPolicyNotFound
	}

	delete(r.policies, policyID)
	r.appendEventLocked(policyID, actor, types.PolicyEventTypeDeleted, map[string]any{
		"name":    policy.Name,
		"version": policy.Version,
	})
	if err := r.saveLocked(); err != nil {
		r.logger.Error("save policy store failed", "err", err)
	}

	return nil
}

func (r *policyRepository) audit(policyID string) []types.PolicyEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	events := make([]types.PolicyEvent, 0, len(r.events[policyID]))
	for _, event := range r.events[policyID] {
		events = append(events, clonePolicyEvent(event))
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].TS.After(events[j].TS)
	})
	return events
}

func (r *policyRepository) insights(rangeDuration time.Duration) types.PolicyInsightsResponse {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if rangeDuration <= 0 {
		rangeDuration = 24 * time.Hour
	}
	rangeStart := time.Now().UTC().Add(-rangeDuration)

	activePolicies := 0
	for _, policy := range r.policies {
		if policy.Status == types.PolicyStatusActive {
			activePolicies++
		}
	}

	triggerCounts := make(map[string]int)
	blocked := 0
	triggered := 0

	for policyID, events := range r.events {
		for _, event := range events {
			if event.Type != types.PolicyEventTypeTriggered || event.TS.Before(rangeStart) {
				continue
			}
			triggerCounts[policyID]++
			triggered++
			if isBlockedOrThrottled(event.Details) {
				blocked++
			}
		}
	}

	var top *types.PolicyInsightsTopPolicy
	for policyID, count := range triggerCounts {
		if count == 0 {
			continue
		}
		policy, ok := r.policies[policyID]
		if !ok {
			continue
		}
		if top == nil || count > top.Triggers {
			top = &types.PolicyInsightsTopPolicy{
				ID:       policy.ID,
				Name:     policy.Name,
				Triggers: count,
			}
		}
	}

	return types.PolicyInsightsResponse{
		ActivePoliciesCount:     activePolicies,
		PoliciesTriggered:       triggered,
		ActionsBlockedThrottled: blocked,
		TopPolicy:               top,
	}
}

func (r *policyRepository) triggerStats(policyID string, rangeDuration time.Duration) (*time.Time, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if rangeDuration <= 0 {
		rangeDuration = 24 * time.Hour
	}
	rangeStart := time.Now().UTC().Add(-rangeDuration)
	last, count, _ := r.computeTriggerStatsLocked(policyID, rangeStart)
	return last, count
}

func (r *policyRepository) computeTriggerStatsLocked(policyID string, rangeStart time.Time) (*time.Time, int, int) {
	events := r.events[policyID]
	var lastTriggeredAt *time.Time
	count := 0
	blocked := 0

	for _, event := range events {
		if event.Type != types.PolicyEventTypeTriggered {
			continue
		}

		ts := event.TS.UTC()
		if lastTriggeredAt == nil || ts.After(*lastTriggeredAt) {
			copyTS := ts
			lastTriggeredAt = &copyTS
		}

		if !ts.Before(rangeStart) {
			count++
			if isBlockedOrThrottled(event.Details) {
				blocked++
			}
		}
	}

	return lastTriggeredAt, count, blocked
}

func (r *policyRepository) appendEventLocked(policyID, actor string, eventType types.PolicyEventType, details map[string]any) {
	event := types.PolicyEvent{
		ID:       uuid.NewString(),
		PolicyID: policyID,
		TS:       time.Now().UTC(),
		Actor:    actor,
		Type:     eventType,
		Details:  cloneMap(details),
	}
	r.events[policyID] = append(r.events[policyID], event)
	if r.eventListener != nil {
		r.eventListener(clonePolicyEvent(event))
	}
}

func (r *policyRepository) nextDuplicateNameLocked(baseName string) string {
	base := strings.TrimSpace(baseName)
	if base == "" {
		base = "Untitled policy"
	}

	candidate := fmt.Sprintf("%s (Copy)", base)
	if !r.nameExistsLocked(candidate) {
		return candidate
	}

	for i := 2; i < 1000; i++ {
		candidate = fmt.Sprintf("%s (Copy %d)", base, i)
		if !r.nameExistsLocked(candidate) {
			return candidate
		}
	}

	return fmt.Sprintf("%s (Copy %d)", base, time.Now().Unix())
}

func (r *policyRepository) nameExistsLocked(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, policy := range r.policies {
		if strings.ToLower(strings.TrimSpace(policy.Name)) == name {
			return true
		}
	}
	return false
}

func (s *Server) handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	filter := policyListFilter{
		Search:     query.Get("search"),
		PipelineID: strings.TrimSpace(query.Get("pipelineId")),
		Range:      parsePolicyRange(query.Get("range")),
		SortBy:     query.Get("sortBy"),
		SortDir:    query.Get("sortDir"),
	}

	if typeVal := strings.TrimSpace(query.Get("type")); typeVal != "" {
		parsed := types.PolicyType(typeVal)
		if !isValidPolicyType(parsed) {
			http.Error(w, "invalid type", http.StatusBadRequest)
			return
		}
		filter.Type = &parsed
	}

	if statusVal := strings.TrimSpace(query.Get("status")); statusVal != "" {
		parsed := types.PolicyStatus(statusVal)
		if !isValidPolicyStatus(parsed) {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		filter.Status = &parsed
	}

	if envVal := strings.TrimSpace(query.Get("env")); envVal != "" {
		parsed := types.PolicyEnvironment(envVal)
		if !isValidPolicyEnvironment(parsed) {
			http.Error(w, "invalid env", http.StatusBadRequest)
			return
		}
		filter.Env = &parsed
	}

	result := s.policies.list(filter)
	writeJSON(w, result, http.StatusOK)
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req upsertPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if err := validateUpsertPolicyRequest(req, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	actor := s.resolvePolicyActor(r.Context())
	policy, err := s.policies.create(req, actor)
	if err != nil {
		http.Error(w, "failed to create policy", http.StatusInternalServerError)
		return
	}

	writeJSON(w, policy, http.StatusCreated)
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	policyID := chi.URLParam(r, "id")
	policy, ok := s.policies.get(policyID)
	if !ok {
		http.Error(w, "policy not found", http.StatusNotFound)
		return
	}

	rangeDuration := parsePolicyRange(r.URL.Query().Get("range"))
	lastTriggeredAt, triggerCount := s.policies.triggerStats(policyID, rangeDuration)

	writeJSON(w, policyDetailResponse{
		Policy:              policy,
		LastTriggeredAt:     lastTriggeredAt,
		TriggerCountInRange: triggerCount,
	}, http.StatusOK)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := chi.URLParam(r, "id")

	var req upsertPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if err := validateUpsertPolicyRequest(req, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	actor := s.resolvePolicyActor(r.Context())
	policy, err := s.policies.update(policyID, req, actor)
	if err != nil {
		if errors.Is(err, errPolicyNotFound) {
			http.Error(w, "policy not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update policy", http.StatusInternalServerError)
		return
	}

	writeJSON(w, policy, http.StatusOK)
}

func (s *Server) handleDuplicatePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := chi.URLParam(r, "id")
	actor := s.resolvePolicyActor(r.Context())

	duplicated, err := s.policies.duplicate(policyID, actor)
	if err != nil {
		if errors.Is(err, errPolicyNotFound) {
			http.Error(w, "policy not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to duplicate policy", http.StatusInternalServerError)
		return
	}

	writeJSON(w, duplicated, http.StatusCreated)
}

func (s *Server) handleEnablePolicy(w http.ResponseWriter, r *http.Request) {
	s.handlePolicyStatusTransition(w, r, types.PolicyStatusDisabled, types.PolicyStatusActive, types.PolicyEventTypeEnabled)
}

func (s *Server) handleDisablePolicy(w http.ResponseWriter, r *http.Request) {
	s.handlePolicyStatusTransition(w, r, "", types.PolicyStatusDisabled, types.PolicyEventTypeDisabled)
}

func (s *Server) handlePausePolicy(w http.ResponseWriter, r *http.Request) {
	s.handlePolicyStatusTransition(w, r, types.PolicyStatusActive, types.PolicyStatusPaused, types.PolicyEventTypePaused)
}

func (s *Server) handleResumePolicy(w http.ResponseWriter, r *http.Request) {
	s.handlePolicyStatusTransition(w, r, types.PolicyStatusPaused, types.PolicyStatusActive, types.PolicyEventTypeResumed)
}

func (s *Server) handlePolicyStatusTransition(
	w http.ResponseWriter,
	r *http.Request,
	requiredCurrent types.PolicyStatus,
	targetStatus types.PolicyStatus,
	eventType types.PolicyEventType,
) {
	policyID := chi.URLParam(r, "id")

	currentPolicy, ok := s.policies.get(policyID)
	if !ok {
		http.Error(w, "policy not found", http.StatusNotFound)
		return
	}

	if requiredCurrent != "" && currentPolicy.Status != requiredCurrent {
		http.Error(w, fmt.Sprintf("policy must be %s", requiredCurrent), http.StatusBadRequest)
		return
	}

	actor := s.resolvePolicyActor(r.Context())
	updatedPolicy, err := s.policies.setStatus(policyID, targetStatus, actor, eventType)
	if err != nil {
		if errors.Is(err, errPolicyNotFound) {
			http.Error(w, "policy not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update status", http.StatusInternalServerError)
		return
	}

	writeJSON(w, updatedPolicy, http.StatusOK)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := chi.URLParam(r, "id")
	actor := s.resolvePolicyActor(r.Context())

	if err := s.policies.delete(policyID, actor); err != nil {
		if errors.Is(err, errPolicyNotFound) {
			http.Error(w, "policy not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete policy", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetPolicyAudit(w http.ResponseWriter, r *http.Request) {
	policyID := chi.URLParam(r, "id")
	events := s.policies.audit(policyID)
	if len(events) == 0 && !s.policies.exists(policyID) {
		http.Error(w, "policy not found", http.StatusNotFound)
		return
	}

	writeJSON(w, types.PolicyAuditResponse{
		PolicyID: policyID,
		Events:   events,
	}, http.StatusOK)
}

func (s *Server) handleGetPolicyInsights(w http.ResponseWriter, r *http.Request) {
	rangeDuration := parsePolicyRange(r.URL.Query().Get("range"))
	insights := s.policies.insights(rangeDuration)
	writeJSON(w, insights, http.StatusOK)
}

func (s *Server) handleGetPolicyTargetOptions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	pipelines := make([]types.PolicyTargetOption, 0)
	pipelineRows := []struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}{}
	if err := s.store.DB().SelectContext(ctx, &pipelineRows, `
		SELECT id, COALESCE(name, '') AS name
		FROM pipeline
		ORDER BY created_at DESC
		LIMIT 500
	`); err == nil {
		for _, row := range pipelineRows {
			pipelines = append(pipelines, types.PolicyTargetOption{
				ID:   strconv.Itoa(row.ID),
				Name: row.Name,
			})
		}
	}

	stages := []string{}
	_ = s.store.DB().SelectContext(ctx, &stages, `
		SELECT DISTINCT COALESCE(name, '') AS name
		FROM stage
		WHERE COALESCE(name, '') <> ''
		ORDER BY name
		LIMIT 500
	`)

	handlers := []string{}
	_ = s.store.DB().SelectContext(ctx, &handlers, `
		SELECT DISTINCT COALESCE(stage_handler_name, '') AS stage_handler_name
		FROM stage
		WHERE COALESCE(stage_handler_name, '') <> ''
		ORDER BY stage_handler_name
		LIMIT 500
	`)

	tags := []string{}
	_ = s.store.DB().SelectContext(ctx, &tags, `
		SELECT DISTINCT COALESCE(value, '') AS value
		FROM keyword
		WHERE COALESCE(value, '') <> ''
		ORDER BY value
		LIMIT 500
	`)

	writeJSON(w, types.PolicyTargetOptionsResponse{
		Environments: []types.PolicyEnvironment{
			types.PolicyEnvironmentAll,
			types.PolicyEnvironmentProd,
			types.PolicyEnvironmentStaging,
			types.PolicyEnvironmentDev,
		},
		Pipelines: pipelines,
		Stages:    dedupeNonEmpty(stages),
		Handlers:  dedupeNonEmpty(handlers),
		Tags:      dedupeNonEmpty(tags),
	}, http.StatusOK)
}

func (s *Server) handlePreviewPolicyTargets(w http.ResponseWriter, r *http.Request) {
	var req types.PolicyPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if req.Environment == "" {
		req.Environment = types.PolicyEnvironmentAll
	}
	if !isValidPolicyEnvironment(req.Environment) {
		http.Error(w, "invalid environment", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	preview, err := s.previewPolicyMatches(ctx, req)
	if err != nil {
		http.Error(w, "failed to preview matches", http.StatusInternalServerError)
		return
	}

	writeJSON(w, preview, http.StatusOK)
}

func (s *Server) previewPolicyMatches(ctx context.Context, req types.PolicyPreviewRequest) (types.PolicyPreviewResponse, error) {
	pipelineRows := []struct {
		ID          int    `db:"id"`
		Environment string `db:"environment"`
	}{}

	err := s.store.DB().SelectContext(ctx, &pipelineRows, `
		SELECT p.id,
			COALESCE(MAX(CASE WHEN LOWER(pci.key) IN ('environment', 'env') THEN LOWER(pci.value) END), '') AS environment
		FROM pipeline p
		LEFT JOIN pipeline_context_item pci ON pci.pipeline_id = p.id
		GROUP BY p.id
	`)
	if err != nil {
		return types.PolicyPreviewResponse{}, err
	}

	tagRows := []struct {
		PipelineID int    `db:"pipeline_id"`
		Tag        string `db:"tag"`
	}{}
	_ = s.store.DB().SelectContext(ctx, &tagRows, `
		SELECT pk.pipeline_id, COALESCE(k.value, '') AS tag
		FROM pipeline_keyword pk
		JOIN keyword k ON k.id = pk.keyword_id
	`)

	stageRows := []struct {
		PipelineID int    `db:"pipeline_id"`
		Name       string `db:"name"`
		Handler    string `db:"handler"`
	}{}
	_ = s.store.DB().SelectContext(ctx, &stageRows, `
		SELECT pipeline_id, COALESCE(name, '') AS name, COALESCE(stage_handler_name, '') AS handler
		FROM stage
	`)

	targeting := normalizeTargeting(req.Targeting)
	pipelineFilter := make(map[string]struct{})
	for _, pipelineID := range targeting.Pipelines {
		pipelineFilter[strings.ToLower(pipelineID)] = struct{}{}
	}
	stageFilter := make(map[string]struct{})
	for _, stage := range targeting.Stages {
		stageFilter[strings.ToLower(stage)] = struct{}{}
	}
	handlerFilter := make(map[string]struct{})
	for _, handler := range targeting.Handlers {
		handlerFilter[strings.ToLower(handler)] = struct{}{}
	}
	tagsInclude := make(map[string]struct{})
	for _, tag := range targeting.TagsInclude {
		tagsInclude[strings.ToLower(tag)] = struct{}{}
	}
	tagsExclude := make(map[string]struct{})
	for _, tag := range targeting.TagsExclude {
		tagsExclude[strings.ToLower(tag)] = struct{}{}
	}

	tagsByPipeline := make(map[int]map[string]struct{})
	for _, row := range tagRows {
		tag := strings.ToLower(strings.TrimSpace(row.Tag))
		if tag == "" {
			continue
		}
		if _, ok := tagsByPipeline[row.PipelineID]; !ok {
			tagsByPipeline[row.PipelineID] = make(map[string]struct{})
		}
		tagsByPipeline[row.PipelineID][tag] = struct{}{}
	}

	allowedPipelines := make(map[int]struct{})
	for _, row := range pipelineRows {
		pipelineID := strconv.Itoa(row.ID)
		if len(pipelineFilter) > 0 {
			if _, ok := pipelineFilter[strings.ToLower(pipelineID)]; !ok {
				continue
			}
		}

		if req.Environment != types.PolicyEnvironmentAll {
			if strings.ToLower(strings.TrimSpace(row.Environment)) != string(req.Environment) {
				continue
			}
		}

		tags := tagsByPipeline[row.ID]
		if !containsAllTags(tags, tagsInclude) {
			continue
		}
		if containsAnyTag(tags, tagsExclude) {
			continue
		}

		allowedPipelines[row.ID] = struct{}{}
	}

	matchedStages := 0
	handlers := make(map[string]struct{})
	for _, stage := range stageRows {
		if _, ok := allowedPipelines[stage.PipelineID]; !ok {
			continue
		}

		stageName := strings.ToLower(strings.TrimSpace(stage.Name))
		handlerName := strings.ToLower(strings.TrimSpace(stage.Handler))

		if len(stageFilter) > 0 {
			if _, ok := stageFilter[stageName]; !ok {
				continue
			}
		}

		if len(handlerFilter) > 0 {
			if _, ok := handlerFilter[handlerName]; !ok {
				continue
			}
		}

		matchedStages++
		if handlerName != "" {
			handlers[handlerName] = struct{}{}
		}
	}

	return types.PolicyPreviewResponse{
		Pipelines: len(allowedPipelines),
		Stages:    matchedStages,
		Handlers:  len(handlers),
	}, nil
}

func matchesPolicyFilter(policy types.Policy, filter policyListFilter) bool {
	if filter.Type != nil && policy.Type != *filter.Type {
		return false
	}
	if filter.Status != nil && policy.Status != *filter.Status {
		return false
	}
	if filter.Env != nil && policy.Environment != *filter.Env {
		return false
	}
	if filter.PipelineID != "" && !stringSliceContains(policy.Targeting.Pipelines, filter.PipelineID) {
		return false
	}

	if strings.TrimSpace(filter.Search) == "" {
		return true
	}

	term := strings.ToLower(strings.TrimSpace(filter.Search))
	if strings.Contains(strings.ToLower(policy.Name), term) {
		return true
	}
	if policy.Description != nil && strings.Contains(strings.ToLower(*policy.Description), term) {
		return true
	}

	searchBuckets := []string{
		strings.Join(policy.Targeting.Pipelines, " "),
		strings.Join(policy.Targeting.Stages, " "),
		strings.Join(policy.Targeting.Handlers, " "),
		strings.Join(policy.Targeting.TagsInclude, " "),
		strings.Join(policy.Targeting.TagsExclude, " "),
	}
	for _, bucket := range searchBuckets {
		if strings.Contains(strings.ToLower(bucket), term) {
			return true
		}
	}

	return false
}

func sortPolicies(items []types.PolicyListItem, sortBy, sortDir string) {
	sortKey := strings.ToLower(strings.TrimSpace(sortBy))
	if sortKey == "" {
		sortKey = "updatedAt"
	}

	desc := strings.ToLower(strings.TrimSpace(sortDir)) != "asc"

	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]

		var cmp int
		switch sortKey {
		case "triggers":
			if left.TriggerCountInRange == right.TriggerCountInRange {
				cmp = compareTime(left.UpdatedAt, right.UpdatedAt)
			} else {
				cmp = compareInt(left.TriggerCountInRange, right.TriggerCountInRange)
			}
		case "lasttriggered":
			cmp = compareNullableTimes(left.LastTriggeredAt, right.LastTriggeredAt)
		case "updatedat":
			fallthrough
		default:
			cmp = compareTime(left.UpdatedAt, right.UpdatedAt)
		}

		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareNullableTimes(left, right *time.Time) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	return compareTime(*left, *right)
}

func compareTime(left, right time.Time) int {
	if left.Before(right) {
		return -1
	}
	if left.After(right) {
		return 1
	}
	return 0
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func validateUpsertPolicyRequest(req upsertPolicyRequest, isCreate bool) error {
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name is required")
	}
	if !isValidPolicyType(req.Type) {
		return errors.New("type is invalid")
	}
	if req.Status != nil && !isValidPolicyStatus(*req.Status) {
		return errors.New("status is invalid")
	}
	if req.Environment == "" {
		req.Environment = types.PolicyEnvironmentAll
	}
	if !isValidPolicyEnvironment(req.Environment) {
		return errors.New("environment is invalid")
	}

	if err := validateRuleByType(req.Type, req.Rule); err != nil {
		return err
	}

	return nil
}

func validateRuleByType(policyType types.PolicyType, rule types.PolicyRule) error {
	switch policyType {
	case types.PolicyTypeRateLimit:
		if rule.Limit == nil || *rule.Limit <= 0 {
			return errors.New("rate limit must be greater than zero")
		}
		if rule.WindowSeconds == nil || *rule.WindowSeconds <= 0 {
			return errors.New("window seconds must be greater than zero")
		}
		if rule.KeyBy == nil || !isOneOf(*rule.KeyBy, "global", "tenant", "user", "custom") {
			return errors.New("keyBy must be one of: global, tenant, user, custom")
		}
		if rule.Burst != nil && *rule.Burst < 0 {
			return errors.New("burst must be zero or greater")
		}
	case types.PolicyTypeRetry:
		if rule.MaxAttempts == nil || *rule.MaxAttempts <= 0 {
			return errors.New("max attempts must be greater than zero")
		}
		if rule.Backoff == nil || !isOneOf(*rule.Backoff, "fixed", "exponential") {
			return errors.New("backoff must be fixed or exponential")
		}
		if rule.BaseDelayMs == nil || *rule.BaseDelayMs <= 0 {
			return errors.New("base delay must be greater than zero")
		}
		if rule.MaxDelayMs != nil && *rule.MaxDelayMs <= 0 {
			return errors.New("max delay must be greater than zero")
		}
	case types.PolicyTypeTimeout:
		if rule.TimeoutMs == nil || *rule.TimeoutMs <= 0 {
			return errors.New("timeout must be greater than zero")
		}
		if rule.AppliesTo == nil || !isOneOf(*rule.AppliesTo, "step", "external_call") {
			return errors.New("appliesTo must be step or external_call")
		}
	case types.PolicyTypeCircuitBreaker:
		if rule.FailureThreshold == nil || *rule.FailureThreshold <= 0 {
			return errors.New("failure threshold must be greater than zero")
		}
		if rule.WindowSeconds == nil || *rule.WindowSeconds <= 0 {
			return errors.New("window seconds must be greater than zero")
		}
		if rule.OpenSeconds == nil || *rule.OpenSeconds <= 0 {
			return errors.New("open seconds must be greater than zero")
		}
		if rule.HalfOpenMaxCalls == nil || *rule.HalfOpenMaxCalls <= 0 {
			return errors.New("half-open max calls must be greater than zero")
		}
	default:
		return errors.New("unsupported policy type")
	}

	return nil
}

func normalizePolicy(policy types.Policy) types.Policy {
	policy.Name = strings.TrimSpace(policy.Name)
	policy.Description = normalizeDescription(policy.Description)
	if policy.Status == "" {
		policy.Status = types.PolicyStatusActive
	}
	if policy.Environment == "" {
		policy.Environment = types.PolicyEnvironmentAll
	}
	policy.Targeting = normalizeTargeting(policy.Targeting)
	policy.Rule = normalizePolicyRule(policy.Rule)
	if policy.Version <= 0 {
		policy.Version = 1
	}
	if strings.TrimSpace(policy.CreatedBy) == "" {
		policy.CreatedBy = "system"
	}
	if strings.TrimSpace(policy.UpdatedBy) == "" {
		policy.UpdatedBy = policy.CreatedBy
	}
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = time.Now().UTC()
	}
	if policy.UpdatedAt.IsZero() {
		policy.UpdatedAt = policy.CreatedAt
	}
	return policy
}

func normalizeDescription(description *string) *string {
	if description == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*description)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeTargeting(targeting types.PolicyTargeting) types.PolicyTargeting {
	targeting.Pipelines = dedupeNonEmpty(targeting.Pipelines)
	targeting.Stages = dedupeNonEmpty(targeting.Stages)
	targeting.Handlers = dedupeNonEmpty(targeting.Handlers)
	targeting.TagsInclude = dedupeNonEmpty(targeting.TagsInclude)
	targeting.TagsExclude = dedupeNonEmpty(targeting.TagsExclude)
	return targeting
}

func normalizePolicyRule(rule types.PolicyRule) types.PolicyRule {
	normalized := types.PolicyRule{
		Limit:            cloneIntPtr(rule.Limit),
		WindowSeconds:    cloneIntPtr(rule.WindowSeconds),
		KeyBy:            cloneStringPtr(rule.KeyBy),
		Burst:            cloneIntPtr(rule.Burst),
		MaxAttempts:      cloneIntPtr(rule.MaxAttempts),
		Backoff:          cloneStringPtr(rule.Backoff),
		BaseDelayMs:      cloneIntPtr(rule.BaseDelayMs),
		MaxDelayMs:       cloneIntPtr(rule.MaxDelayMs),
		Jitter:           cloneBoolPtr(rule.Jitter),
		TimeoutMs:        cloneIntPtr(rule.TimeoutMs),
		AppliesTo:        cloneStringPtr(rule.AppliesTo),
		FailureThreshold: cloneIntPtr(rule.FailureThreshold),
		OpenSeconds:      cloneIntPtr(rule.OpenSeconds),
		HalfOpenMaxCalls: cloneIntPtr(rule.HalfOpenMaxCalls),
	}

	if rule.RetryOn != nil {
		normalized.RetryOn = &types.RetryOnRule{
			HTTPStatus: append([]int(nil), rule.RetryOn.HTTPStatus...),
			ErrorCodes: dedupeNonEmpty(rule.RetryOn.ErrorCodes),
		}
	}

	if normalized.KeyBy != nil {
		v := strings.ToLower(strings.TrimSpace(*normalized.KeyBy))
		normalized.KeyBy = &v
	}
	if normalized.Backoff != nil {
		v := strings.ToLower(strings.TrimSpace(*normalized.Backoff))
		normalized.Backoff = &v
	}
	if normalized.AppliesTo != nil {
		v := strings.ToLower(strings.TrimSpace(*normalized.AppliesTo))
		normalized.AppliesTo = &v
	}

	return normalized
}

func clonePolicy(policy types.Policy) types.Policy {
	cloned := policy
	cloned.Description = cloneStringPtr(policy.Description)
	cloned.Targeting = types.PolicyTargeting{
		Pipelines:   append([]string(nil), policy.Targeting.Pipelines...),
		Stages:      append([]string(nil), policy.Targeting.Stages...),
		Handlers:    append([]string(nil), policy.Targeting.Handlers...),
		TagsInclude: append([]string(nil), policy.Targeting.TagsInclude...),
		TagsExclude: append([]string(nil), policy.Targeting.TagsExclude...),
	}
	cloned.Rule = normalizePolicyRule(policy.Rule)
	return cloned
}

func clonePolicyEvent(event types.PolicyEvent) types.PolicyEvent {
	cloned := event
	cloned.Details = cloneMap(event.Details)
	return cloned
}

func cloneMap(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(details))
	for key, value := range details {
		cloned[key] = value
	}
	return cloned
}

func cloneIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

func cloneBoolPtr(v *bool) *bool {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

func cloneStringPtr(v *string) *string {
	if v == nil {
		return nil
	}
	copy := strings.TrimSpace(*v)
	return &copy
}

func isValidPolicyType(policyType types.PolicyType) bool {
	switch policyType {
	case types.PolicyTypeRateLimit, types.PolicyTypeRetry, types.PolicyTypeTimeout, types.PolicyTypeCircuitBreaker:
		return true
	default:
		return false
	}
}

func isValidPolicyStatus(status types.PolicyStatus) bool {
	switch status {
	case types.PolicyStatusActive, types.PolicyStatusPaused, types.PolicyStatusDisabled:
		return true
	default:
		return false
	}
}

func isValidPolicyEnvironment(env types.PolicyEnvironment) bool {
	switch env {
	case types.PolicyEnvironmentAll, types.PolicyEnvironmentProd, types.PolicyEnvironmentStaging, types.PolicyEnvironmentDev:
		return true
	default:
		return false
	}
}

func isBlockedOrThrottled(details map[string]any) bool {
	if len(details) == 0 {
		return false
	}

	if blockedRaw, ok := details["blocked"]; ok {
		if blocked, ok := blockedRaw.(bool); ok && blocked {
			return true
		}
	}

	checkReason := func(raw any) bool {
		reason, ok := raw.(string)
		if !ok {
			return false
		}
		reason = strings.ToLower(strings.TrimSpace(reason))
		return strings.Contains(reason, "throttle") ||
			strings.Contains(reason, "circuit") ||
			strings.Contains(reason, "blocked")
	}

	if checkReason(details["reason"]) {
		return true
	}
	if checkReason(details["action"]) {
		return true
	}

	return false
}

func containsAllTags(source map[string]struct{}, required map[string]struct{}) bool {
	if len(required) == 0 {
		return true
	}
	if len(source) == 0 {
		return false
	}
	for tag := range required {
		if _, ok := source[tag]; !ok {
			return false
		}
	}
	return true
}

func containsAnyTag(source map[string]struct{}, denied map[string]struct{}) bool {
	if len(source) == 0 || len(denied) == 0 {
		return false
	}
	for tag := range denied {
		if _, ok := source[tag]; ok {
			return true
		}
	}
	return false
}

func dedupeNonEmpty(values []string) []string {
	if len(values) == 0 {
		return []string{}
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

func parsePolicyRange(raw string) time.Duration {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return 24 * time.Hour
	}

	switch value {
	case "15m", "last_15m", "last15m":
		return 15 * time.Minute
	case "1h", "60m", "last_1h", "last1h":
		return time.Hour
	case "24h", "1d", "last_24h", "last24h":
		return 24 * time.Hour
	case "7d", "168h", "last_7d", "last7d":
		return 7 * 24 * time.Hour
	}

	if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
		return parsed
	}

	return 24 * time.Hour
}

func isOneOf(value string, options ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func stringSliceContains(items []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, item := range items {
		if strings.ToLower(strings.TrimSpace(item)) == value {
			return true
		}
	}
	return false
}

func (s *Server) resolvePolicyActor(ctx context.Context) string {
	userID := getUserIDFromContext(ctx)
	if userID == 0 {
		return "system"
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Sprintf("user:%d", userID)
	}

	fullName := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + stringValue(user.LastName))
	if fullName != "" {
		return fullName
	}
	if strings.TrimSpace(user.Email) != "" {
		return user.Email
	}
	return fmt.Sprintf("user:%d", userID)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (s *Server) registerPolicyRoutes(r chi.Router) {
	r.Get("/", s.handleGetPolicies)
	r.Post("/", s.handleCreatePolicy)
	r.Get("/insights", s.handleGetPolicyInsights)
	r.Get("/targets", s.handleGetPolicyTargetOptions)
	r.Post("/preview", s.handlePreviewPolicyTargets)

	r.Get("/{id}", s.handleGetPolicy)
	r.Put("/{id}", s.handleUpdatePolicy)
	r.Delete("/{id}", s.handleDeletePolicy)
	r.Get("/{id}/audit", s.handleGetPolicyAudit)
	r.Post("/{id}/duplicate", s.handleDuplicatePolicy)
	r.Post("/{id}/enable", s.handleEnablePolicy)
	r.Post("/{id}/disable", s.handleDisablePolicy)
	r.Post("/{id}/pause", s.handlePausePolicy)
	r.Post("/{id}/resume", s.handleResumePolicy)
}
