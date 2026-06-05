package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"pipelogiq/internal/alerts"
	"pipelogiq/internal/config"
	"pipelogiq/internal/constants"
	"pipelogiq/internal/mq"
	observabilityhttp "pipelogiq/internal/observability/http"
	observabilityrepo "pipelogiq/internal/observability/repo"
	observabilityservice "pipelogiq/internal/observability/service"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
	"pipelogiq/internal/version"
)

type Server struct {
	cfg                  config.APIConfig
	store                *store.Store
	mq                   *mq.Client
	hub                  *Hub
	policies             *policyRepository
	dbPolicies           *dbPolicyRepository
	observabilityHandler *observabilityhttp.Handler
	logger               *slog.Logger
	server               *http.Server
	jwtSecret            []byte
	allowedOrigins       map[string]struct{}
}

func NewServer(cfg config.APIConfig, st *store.Store, mqClient *mq.Client, logger *slog.Logger) *Server {
	policiesRepo := newPolicyRepository(logger)
	return NewServerWithPolicies(cfg, st, mqClient, logger, policiesRepo)
}

func NewServerWithPolicies(cfg config.APIConfig, st *store.Store, mqClient *mq.Client, logger *slog.Logger, policiesRepo *policyRepository) *Server {
	return NewServerWithPoliciesAndDB(cfg, st, mqClient, logger, policiesRepo, nil)
}

func NewServerWithPoliciesAndDB(cfg config.APIConfig, st *store.Store, mqClient *mq.Client, logger *slog.Logger, policiesRepo *policyRepository, dbPoliciesRepo *dbPolicyRepository) *Server {
	observabilityRepo := observabilityrepo.NewSQLRepository(st.DB())
	observabilitySvc := observabilityservice.New(observabilityRepo, logger)
	observabilityHandler := observabilityhttp.NewHandler(observabilitySvc, logger)
	alertsNotifier := alerts.New(observabilityRepo, logger)
	st.SetAlertSink(alertsNotifier)
	policiesRepo.setEventListener(func(event types.PolicyEvent) {
		go func(ev types.PolicyEvent) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			alertsNotifier.NotifyPolicyEvent(ctx, ev)
		}(event)
	})

	return &Server{
		cfg:                  cfg,
		store:                st,
		mq:                   mqClient,
		hub:                  NewHub(logger, cfg.AllowedOrigins),
		policies:             policiesRepo,
		dbPolicies:           dbPoliciesRepo,
		observabilityHandler: observabilityHandler,
		logger:               logger,
		jwtSecret:            []byte(cfg.JWTSecret),
		allowedOrigins:       allowedOriginsMap(cfg.AllowedOrigins),
	}
}

func (s *Server) Run(ctx context.Context) error {
	router := chi.NewRouter()

	// Global middleware
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(60 * time.Second))
	router.Use(otelhttp.NewMiddleware("pipelogiq-api-internal"))
	router.Use(s.corsMiddleware)

	// Health and version endpoints
	router.Get(s.cfg.HealthLivenessEndpoint, s.handleHealth)
	router.Get(s.cfg.HealthReadyEndpoint, s.handleHealth)
	router.Get("/version", version.HandleVersion)
	router.Handle("/metrics", promhttp.Handler())

	// Auth endpoints (public)
	router.Post("/auth/login", s.handleLogin)
	router.Post("/auth/logout", s.handleLogout)

	// All other endpoints require auth
	router.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		// Auth
		r.Get("/auth/me", s.handleGetCurrentUser)
		r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
			s.hub.ServeWS(w, r)
		})

		// Pipeline endpoints
		r.Get("/pipelines/{id}", s.handleGetPipeline)
		r.Get("/pipelines/{id}/stages", s.handleGetStages)
		r.Get("/pipelines/{id}/context", s.handleGetContext)
		r.Get("/pipelines", s.handleGetPipelines)
		r.Post("/pipelines/{id}/pause", s.handlePausePipeline)
		r.Post("/pipelines/{id}/resume", s.handleResumePipeline)
		r.Post("/pipelines/bulkAction", s.handleBulkPipelineAction)
		r.Post("/pipelines/rerunStage", s.handleRerunStage)
		r.Post("/pipelines/skipStage", s.handleSkipStage)
		r.Get("/pipelines/logs/{pipelineId}", s.handleGetPipelineLogs)
		r.Get("/pipelines/logs/{pipelineId}/{stageId}", s.handleGetPipelineLogs)
		r.Get("/pipelines/stages/{pipelineId}", s.handleGetPipelineStagesAlt)
		r.Get("/pipelines/context/{pipelineId}", s.handleGetPipelineContextAlt)

		// Keywords
		r.Get("/keywords", s.handleGetKeywords)

		r.Group(func(admin chi.Router) {
			admin.Use(s.requireAdminMiddleware)

			// Application endpoints
			admin.Get("/applications", s.handleGetApplications)
			admin.Post("/applications", s.handleSaveApplication)

			// ApiKey endpoints
			admin.Post("/apiKeys", s.handleGenerateApiKey)
			admin.Get("/apiKeys", s.handleGetApiKeys)
			admin.Put("/apiKeys/disable", s.handleDisableApiKey)

			// Operational endpoints
			admin.Get("/logs/{appId}", s.handleGetLogsByAppID)
			admin.Get("/workers", s.handleGetWorkers)
			admin.Get("/workers/events", s.handleGetWorkerEvents)
			admin.Get("/workers/{workerId}/events", s.handleGetWorkerEvents)

			// Observability endpoints
			admin.Route("/observability", s.registerObservabilityRoutes)

			// Policy endpoints
			admin.Route("/policies", s.registerPolicyRoutes)
		})
	})

	s.server = &http.Server{
		Addr:    s.cfg.HTTPAddr,
		Handler: router,
	}

	// Subscribe to StageUpdated fanout exchange and broadcast to WebSocket clients
	go func() {
		const exchange = constants.StageUpdated + ".fanout"
		s.logger.Info("starting StageUpdated fanout subscriber", "exchange", exchange)
		if err := s.mq.SubscribeFanout(ctx, exchange, func(_ context.Context, body []byte) {
			s.hub.Broadcast(body)
		}); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error("fanout subscriber exited", "err", err)
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("api listening", "addr", s.cfg.HTTPAddr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return newCORSMiddleware(s.allowedOrigins)(next)
}

func newCORSMiddleware(allowedOrigins map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			if !isAllowedOrigin(allowedOrigins, origin) {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Requested-With")
			w.Header().Set("Vary", "Origin")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedOrigin(allowedOrigins map[string]struct{}, origin string) bool {
	_, ok := allowedOrigins[strings.TrimSpace(origin)]
	return ok
}

func allowedOriginsMap(origins []string) map[string]struct{} {
	out := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	return out
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	pipeline, err := s.store.GetPipelineFullDetail(ctx, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, pipeline, http.StatusOK)
}

func (s *Server) handleGetStages(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stages, err := s.store.GetPipelineStages(ctx, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, stages, http.StatusOK)
}

func (s *Server) handleGetContext(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	ctxItems, err := s.store.GetPipelineContext(ctx, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, ctxItems, http.StatusOK)
}

// Alternative routes matching .NET paths
func (s *Server) handleGetPipelineStagesAlt(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "pipelineId")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stages, err := s.store.GetPipelineStages(ctx, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, stages, http.StatusOK)
}

func (s *Server) handleGetPipelineContextAlt(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "pipelineId")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	ctxItems, err := s.store.GetPipelineContext(ctx, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, ctxItems, http.StatusOK)
}

func writeJSON(w http.ResponseWriter, payload any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
