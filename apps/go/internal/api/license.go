package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/license"
	"pipelogiq/internal/pro"
)

const problemTypeLicenseRequired = "https://api.pipelogiq.dev/errors/license-required"

// requireFeature is the whole gate. A handler calls it once and returns.
//
// It never touches pipeline execution: gating happens at the API surface,
// before work is created. Nothing in the execution path consults the licence, so
// an expired licence cannot stop a running pipeline.
func (s *Server) requireFeature(w http.ResponseWriter, r *http.Request, feature string) bool {
	if license.Has(feature) {
		return true
	}

	detail := fmt.Sprintf("%s requires a Pro licence", license.FeatureDescriptions[feature])
	if status := license.Current(); status.Reason != "" {
		detail = fmt.Sprintf("%s. %s", detail, status.Reason)
	}

	writeProblemJSON(w, r, http.StatusPaymentRequired,
		problemTypeLicenseRequired,
		"Licence required",
		detail,
		map[string][]string{"feature": {feature}})
	return false
}

// licenseResponse is what the Settings page reads once on load. It renders locked
// features with an explanation rather than hiding them.
type licenseResponse struct {
	license.Status
	// ProCompiled reports whether the Pro implementations are linked into this
	// binary. Diagnostic only: a licence still has to open each gate.
	ProCompiled bool             `json:"proCompiled"`
	Catalogue   []featureListing `json:"catalogue"`
}

type featureListing struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unlocked    bool   `json:"unlocked"`
	Available   bool   `json:"available"`
}

func (s *Server) handleGetLicense(w http.ResponseWriter, r *http.Request) {
	status := license.Current()

	catalogue := make([]featureListing, 0, len(license.AllFeatures))
	for _, feature := range license.AllFeatures {
		catalogue = append(catalogue, featureListing{
			Name:        feature,
			Description: license.FeatureDescriptions[feature],
			Unlocked:    status.Has(feature),
			Available:   isImplementedFeature(feature),
		})
	}

	writeJSON(w, licenseResponse{
		Status:      status,
		ProCompiled: s.pro.Compiled,
		Catalogue:   catalogue,
	}, http.StatusOK)
}

// implementedFeatures are the gates that have code behind them today. The rest
// exist as names so the pricing page and the code cannot drift.
var implementedFeatures = map[string]struct{}{
	license.FeatureSSO:       {},
	license.FeatureAudit:     {},
	license.FeatureRetention: {},
}

func isImplementedFeature(feature string) bool {
	_, ok := implementedFeatures[feature]
	return ok
}

// registerLicenseRoutes mounts the licensed surface. Every handler gates first
// and does nothing else until the gate opens.
func (s *Server) registerLicenseRoutes(r chi.Router) {
	r.Get("/license", s.handleGetLicense)
	r.Get("/audit/export", s.handleAuditExport)
	r.Get("/retention/{applicationId}", s.handleGetRetentionPolicy)
	r.Put("/retention/{applicationId}", s.handleSetRetentionPolicy)
	r.Post("/retention/{applicationId}/purge", s.handlePurgeRetention)
}

func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireFeature(w, r, license.FeatureAudit) {
		return
	}

	applicationID, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("applicationId")))
	if err != nil || applicationID <= 0 {
		writeJSONError(w, "applicationId is required", http.StatusBadRequest)
		return
	}
	if !s.requireApplicationAccess(w, r, applicationID) {
		return
	}

	from, to, err := parseExportRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="audit-%d-%s-%s.ndjson"`,
			applicationID, from.Format("20060102"), to.Format("20060102")))

	result, err := s.pro.Audit.Export(ctx, pro.AuditExportRequest{
		ApplicationID: applicationID,
		From:          from,
		To:            to,
	}, w)
	if err != nil {
		// The response is already streaming, so the status is committed. Log it
		// and let the missing trailer be the signal that the file is incomplete.
		s.logger.Error("audit export failed",
			"applicationId", applicationID, "err", err)
		return
	}
	s.logger.Info("audit export completed",
		"applicationId", applicationID,
		"records", result.Records,
		"algorithm", result.Algorithm)
}

func parseExportRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30)
	to := now

	var err error
	if trimmed := strings.TrimSpace(fromRaw); trimmed != "" {
		if from, err = time.Parse(time.RFC3339, trimmed); err != nil {
			return time.Time{}, time.Time{}, errors.New("from must be RFC3339")
		}
	}
	if trimmed := strings.TrimSpace(toRaw); trimmed != "" {
		if to, err = time.Parse(time.RFC3339, trimmed); err != nil {
			return time.Time{}, time.Time{}, errors.New("to must be RFC3339")
		}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, errors.New("the export range ends before it starts")
	}
	return from.UTC(), to.UTC(), nil
}

func (s *Server) handleGetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := s.retentionRequestContext(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	policy, err := s.pro.Retention.Policy(ctx, applicationID)
	if err != nil {
		s.logger.Error("load retention policy failed", "applicationId", applicationID, "err", err)
		writeJSONError(w, "failed to load the retention policy", http.StatusInternalServerError)
		return
	}
	writeJSON(w, policy, http.StatusOK)
}

func (s *Server) handleSetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := s.retentionRequestContext(w, r)
	if !ok {
		return
	}

	var policy pro.RetentionPolicy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&policy); err != nil {
		writeJSONError(w, "invalid payload", http.StatusBadRequest)
		return
	}
	// The route is the authority on which application is being configured.
	policy.ApplicationID = applicationID

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.pro.Retention.SetPolicy(ctx, policy); err != nil {
		if errors.Is(err, pro.ErrNotConfigured) {
			writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		// A rejected window is the caller's mistake, not a server fault.
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.logger.Info("retention policy updated",
		"applicationId", applicationID,
		"keepDays", policy.KeepDays,
		"enabled", policy.Enabled)
	writeJSON(w, policy, http.StatusOK)
}

func (s *Server) handlePurgeRetention(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := s.retentionRequestContext(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := s.pro.Retention.Purge(ctx, applicationID, time.Now().UTC())
	if err != nil {
		s.logger.Error("retention purge failed", "applicationId", applicationID, "err", err)
		writeJSONError(w, "failed to purge", http.StatusInternalServerError)
		return
	}
	s.logger.Warn("retention purge completed",
		"applicationId", applicationID,
		"pipelines", result.Pipelines,
		"stages", result.Stages,
		"logs", result.Logs)
	writeJSON(w, result, http.StatusOK)
}

// retentionRequestContext gates on the licence and on membership, in that order.
func (s *Server) retentionRequestContext(w http.ResponseWriter, r *http.Request) (int, bool) {
	if !s.requireFeature(w, r, license.FeatureRetention) {
		return 0, false
	}

	applicationID, err := parseRouteID(chi.URLParam(r, "applicationId"))
	if err != nil {
		writeJSONError(w, "applicationId must be a positive integer", http.StatusBadRequest)
		return 0, false
	}
	if !s.requireApplicationAccess(w, r, applicationID) {
		return 0, false
	}
	return applicationID, true
}
