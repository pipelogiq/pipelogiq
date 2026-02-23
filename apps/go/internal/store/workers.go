package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

var errWorkerSessionInvalid = errors.New("invalid worker session")

func IsInvalidWorkerSessionError(err error) bool {
	return errors.Is(err, errWorkerSessionInvalid)
}

type workerClientSnapshot struct {
	ID               string          `db:"id"`
	ApplicationID    int             `db:"application_id"`
	ApplicationName  string          `db:"application_name"`
	AppRuntimeID     string          `db:"app_runtime_id"`
	WorkerName       string          `db:"worker_name"`
	InstanceID       string          `db:"instance_id"`
	WorkerVersion    sql.NullString  `db:"worker_version"`
	SDKVersion       sql.NullString  `db:"sdk_version"`
	Environment      sql.NullString  `db:"environment"`
	HostName         sql.NullString  `db:"host_name"`
	PID              sql.NullInt32   `db:"pid"`
	State            string          `db:"state"`
	StatusReason     sql.NullString  `db:"status_reason"`
	BrokerType       sql.NullString  `db:"broker_type"`
	BrokerConnected  bool            `db:"broker_connected"`
	InFlightJobs     int             `db:"in_flight_jobs"`
	JobsProcessed    int64           `db:"jobs_processed"`
	JobsFailed       int64           `db:"jobs_failed"`
	QueueLag         sql.NullInt32   `db:"queue_lag"`
	CPUPercent       sql.NullFloat64 `db:"cpu_percent"`
	MemoryMB         sql.NullFloat64 `db:"memory_mb"`
	LastError        sql.NullString  `db:"last_error"`
	StartedAt        time.Time       `db:"started_at"`
	LastSeenAt       time.Time       `db:"last_seen_at"`
	StoppedAt        sql.NullTime    `db:"stopped_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
	SupportedJSON    string          `db:"supported_handlers_json"`
	CapabilitiesJSON string          `db:"capabilities_json"`
	MetadataJSON     string          `db:"metadata_json"`
	SessionExpiresAt time.Time       `db:"session_expires_at"`
}

func (s *Store) GetApplicationNameByID(ctx context.Context, appID int) (string, error) {
	var name string
	if err := s.db.GetContext(ctx, &name, `SELECT name FROM application WHERE id = $1`, appID); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Store) GetObservabilityLinkTemplates(ctx context.Context) (string, string, error) {
	type row struct {
		Type       string `db:"type"`
		ConfigJSON string `db:"config_json"`
	}
	rows := []row{}
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT type, config_json
		FROM observability_integration_config
		WHERE type IN ('opentelemetry', 'graylog')
	`); err != nil {
		return "", "", err
	}

	traceTemplate := ""
	logsTemplate := ""
	for _, row := range rows {
		config := map[string]any{}
		if strings.TrimSpace(row.ConfigJSON) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(row.ConfigJSON), &config); err != nil {
			continue
		}

		switch row.Type {
		case "opentelemetry":
			if value, ok := config["traceLinkTemplate"].(string); ok {
				traceTemplate = strings.TrimSpace(value)
			}
		case "graylog":
			if value, ok := config["searchUrlTemplate"].(string); ok {
				logsTemplate = strings.TrimSpace(value)
			}
		}
	}

	return traceTemplate, logsTemplate, nil
}

func (s *Store) RegisterWorkerSession(
	ctx context.Context,
	appID int,
	appRuntimeID string,
	brokerType string,
	req types.WorkerBootstrapRequest,
	sessionToken string,
	sessionExpiresAt time.Time,
) (string, error) {
	workerID := uuid.NewString()
	now := time.Now().UTC()

	workerName := strings.TrimSpace(req.WorkerName)
	if workerName == "" {
		workerName = "worker"
	}

	instanceID := strings.TrimSpace(req.InstanceID)
	if instanceID == "" {
		instanceID = uuid.NewString()
	}

	supportedJSON, err := toJSONText(req.SupportedHandlers, "[]")
	if err != nil {
		return "", err
	}
	capabilitiesJSON, err := toJSONText(req.Capabilities, "{}")
	if err != nil {
		return "", err
	}
	metadataJSON, err := toJSONText(req.Metadata, "{}")
	if err != nil {
		return "", err
	}

	query := `
		INSERT INTO worker_client (
			id,
			application_id,
			app_runtime_id,
			worker_name,
			instance_id,
			worker_version,
			sdk_version,
			environment,
			host_name,
			pid,
			state,
			broker_type,
			broker_connected,
			in_flight_jobs,
			jobs_processed,
			jobs_failed,
			supported_handlers_json,
			capabilities_json,
			metadata_json,
			session_token,
			session_expires_at,
			started_at,
			last_seen_at,
			created_at,
			updated_at,
			stopped_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, false, 0, 0, 0, $13, $14, $15, $16, $17, $18, $18, $18, $18, NULL
		)
		ON CONFLICT (application_id, instance_id) DO UPDATE SET
			app_runtime_id = EXCLUDED.app_runtime_id,
			worker_name = EXCLUDED.worker_name,
			worker_version = EXCLUDED.worker_version,
			sdk_version = EXCLUDED.sdk_version,
			environment = EXCLUDED.environment,
			host_name = EXCLUDED.host_name,
			pid = EXCLUDED.pid,
			state = EXCLUDED.state,
			status_reason = NULL,
			broker_type = EXCLUDED.broker_type,
			broker_connected = false,
			in_flight_jobs = 0,
			queue_lag = NULL,
			cpu_percent = NULL,
			memory_mb = NULL,
			last_error = NULL,
			supported_handlers_json = EXCLUDED.supported_handlers_json,
			capabilities_json = EXCLUDED.capabilities_json,
			metadata_json = EXCLUDED.metadata_json,
			session_token = EXCLUDED.session_token,
			session_expires_at = EXCLUDED.session_expires_at,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at,
			stopped_at = NULL
		RETURNING id
	`

	pid := nullableInt(req.PID)
	var persistedID string
	if err := s.db.GetContext(ctx, &persistedID, query,
		workerID,
		appID,
		strings.TrimSpace(appRuntimeID),
		workerName,
		instanceID,
		nullableStringVal(req.WorkerVersion),
		nullableStringVal(req.SDKVersion),
		nullableStringVal(req.Environment),
		nullableStringVal(req.HostName),
		pid,
		types.WorkerStateStarting,
		strings.ToLower(strings.TrimSpace(brokerType)),
		supportedJSON,
		capabilitiesJSON,
		metadataJSON,
		sessionToken,
		sessionExpiresAt.UTC(),
		now,
	); err != nil {
		return "", err
	}

	bootstrapDetails := map[string]any{
		"workerName":    workerName,
		"instanceId":    instanceID,
		"workerVersion": strings.TrimSpace(req.WorkerVersion),
		"sdkVersion":    strings.TrimSpace(req.SDKVersion),
		"environment":   strings.TrimSpace(req.Environment),
		"hostName":      strings.TrimSpace(req.HostName),
	}
	_ = s.insertWorkerEvent(ctx, persistedID, now, "INFO", "worker.bootstrap", "Worker bootstrap completed", bootstrapDetails)
	s.emitWorkerAlert(WorkerAlertEvent{
		WorkerID:  persistedID,
		TS:        now.UTC(),
		Level:     "INFO",
		EventType: "worker.bootstrap",
		Message:   "Worker bootstrap completed",
		Details:   cloneAlertDetailsMap(bootstrapDetails),
	})

	return persistedID, nil
}

func (s *Store) UpdateWorkerHeartbeat(ctx context.Context, token string, req types.WorkerHeartbeatRequest) error {
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" || strings.TrimSpace(token) == "" {
		return errWorkerSessionInvalid
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var snapshot workerClientSnapshot
	selectQuery := `
		SELECT
			wc.id,
			wc.application_id,
			a.name AS application_name,
			wc.app_runtime_id,
			wc.worker_name,
			wc.instance_id,
			wc.worker_version,
			wc.sdk_version,
			wc.environment,
			wc.host_name,
			wc.pid,
			wc.state,
			wc.status_reason,
			wc.broker_type,
			wc.broker_connected,
			wc.in_flight_jobs,
			wc.jobs_processed,
			wc.jobs_failed,
			wc.queue_lag,
			wc.cpu_percent,
			wc.memory_mb,
			wc.last_error,
			wc.started_at,
			wc.last_seen_at,
			wc.stopped_at,
			wc.updated_at,
			wc.supported_handlers_json,
			wc.capabilities_json,
			wc.metadata_json,
			wc.session_expires_at
		FROM worker_client wc
		JOIN application a ON a.id = wc.application_id
		WHERE wc.id = $1 AND wc.session_token = $2
		LIMIT 1
	`
	if err = tx.GetContext(ctx, &snapshot, selectQuery, workerID, token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errWorkerSessionInvalid
		}
		return err
	}

	now := time.Now().UTC()
	if snapshot.SessionExpiresAt.Before(now) {
		return errWorkerSessionInvalid
	}

	nextState := sanitizeWorkerState(req.State, snapshot.State)
	brokerConnected := snapshot.BrokerConnected
	if req.BrokerConnected != nil {
		brokerConnected = *req.BrokerConnected
	}
	inFlightJobs := snapshot.InFlightJobs
	if req.InFlightJobs != nil {
		inFlightJobs = maxInt(*req.InFlightJobs, 0)
	}
	jobsProcessed := snapshot.JobsProcessed
	if req.JobsProcessed != nil {
		jobsProcessed = maxInt64(*req.JobsProcessed, 0)
	}
	jobsFailed := snapshot.JobsFailed
	if req.JobsFailed != nil {
		jobsFailed = maxInt64(*req.JobsFailed, 0)
	}

	var queueLag any
	if req.QueueLag != nil {
		value := maxInt(*req.QueueLag, 0)
		queueLag = value
	} else if snapshot.QueueLag.Valid {
		queueLag = int(snapshot.QueueLag.Int32)
	}

	var cpuPercent any
	if req.CPUPercent != nil {
		cpuPercent = *req.CPUPercent
	} else if snapshot.CPUPercent.Valid {
		cpuPercent = snapshot.CPUPercent.Float64
	}

	var memoryMB any
	if req.MemoryMB != nil {
		memoryMB = *req.MemoryMB
	} else if snapshot.MemoryMB.Valid {
		memoryMB = snapshot.MemoryMB.Float64
	}

	lastError := snapshot.LastError.String
	if req.LastError != nil {
		lastError = strings.TrimSpace(*req.LastError)
	}
	if lastError == "" {
		lastError = ""
	}

	statusReason := snapshot.StatusReason.String
	if req.Message != nil {
		statusReason = strings.TrimSpace(*req.Message)
	}
	if statusReason == "" {
		statusReason = ""
	}

	metadataJSON, marshalErr := toJSONText(req.Metadata, snapshot.MetadataJSON)
	if marshalErr != nil {
		return marshalErr
	}

	var stoppedAt any
	if nextState == types.WorkerStateStopped {
		stoppedAt = now
	} else {
		stoppedAt = nil
	}

	updateQuery := `
		UPDATE worker_client
		SET
			state = $3,
			status_reason = $4,
			broker_connected = $5,
			in_flight_jobs = $6,
			jobs_processed = $7,
			jobs_failed = $8,
			queue_lag = $9,
			cpu_percent = $10,
			memory_mb = $11,
			last_error = $12,
			metadata_json = $13,
			last_seen_at = $14,
			updated_at = $14,
			stopped_at = $15
		WHERE id = $1 AND session_token = $2
	`

	if _, err = tx.ExecContext(ctx, updateQuery,
		workerID,
		token,
		nextState,
		nullableStringVal(statusReason),
		brokerConnected,
		inFlightJobs,
		jobsProcessed,
		jobsFailed,
		queueLag,
		cpuPercent,
		memoryMB,
		nullableStringVal(lastError),
		metadataJSON,
		now,
		stoppedAt,
	); err != nil {
		return err
	}

	heartbeatPayload := map[string]any{
		"uptimeSec": req.UptimeSec,
		"message":   req.Message,
		"metadata":  req.Metadata,
	}
	payloadJSON, marshalErr := toJSONText(heartbeatPayload, "{}")
	if marshalErr != nil {
		return marshalErr
	}

	insertHeartbeat := `
		INSERT INTO worker_heartbeat (
			worker_id,
			ts,
			state,
			broker_connected,
			in_flight_jobs,
			jobs_processed,
			jobs_failed,
			queue_lag,
			cpu_percent,
			memory_mb,
			last_error,
			payload_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	if _, err = tx.ExecContext(ctx, insertHeartbeat,
		workerID,
		now,
		nextState,
		brokerConnected,
		inFlightJobs,
		jobsProcessed,
		jobsFailed,
		queueLag,
		cpuPercent,
		memoryMB,
		nullableStringVal(lastError),
		payloadJSON,
	); err != nil {
		return err
	}

	stateChanged := snapshot.State != nextState
	stateChangeDetails := map[string]any{
		"from": snapshot.State,
		"to":   nextState,
	}
	if stateChanged {
		if err = insertWorkerEventTx(ctx, tx, workerID, now, "INFO", "worker.state_changed",
			fmt.Sprintf("Worker state changed from %s to %s", snapshot.State, nextState),
			stateChangeDetails,
		); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	if stateChanged {
		s.emitWorkerAlert(WorkerAlertEvent{
			WorkerID:  workerID,
			TS:        now.UTC(),
			Level:     "INFO",
			EventType: "worker.state_changed",
			Message:   fmt.Sprintf("Worker state changed from %s to %s", snapshot.State, nextState),
			Details:   cloneAlertDetailsMap(stateChangeDetails),
		})
	}
	return nil
}

func (s *Store) SaveWorkerEvents(
	ctx context.Context,
	workerID string,
	token string,
	events []types.WorkerEventInput,
) error {
	workerID = strings.TrimSpace(workerID)
	token = strings.TrimSpace(token)
	if workerID == "" || token == "" {
		return errWorkerSessionInvalid
	}

	var expiresAt time.Time
	err := s.db.GetContext(ctx, &expiresAt, `
		SELECT session_expires_at
		FROM worker_client
		WHERE id = $1 AND session_token = $2
		LIMIT 1
	`, workerID, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errWorkerSessionInvalid
		}
		return err
	}
	if expiresAt.Before(time.Now().UTC()) {
		return errWorkerSessionInvalid
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC()
	alertEvents := make([]WorkerAlertEvent, 0, len(events))
	for _, event := range events {
		eventTS := now
		if event.TS != nil {
			eventTS = event.TS.UTC()
		}
		level := normalizeLogLevel(event.Level)
		eventType := strings.TrimSpace(event.EventType)
		if eventType == "" {
			eventType = "worker.event"
		}
		message := strings.TrimSpace(event.Message)
		if message == "" {
			message = "worker event"
		}
		if err = insertWorkerEventTx(ctx, tx, workerID, eventTS, level, eventType, message, event.Details); err != nil {
			return err
		}
		alertEvents = append(alertEvents, WorkerAlertEvent{
			WorkerID:  workerID,
			TS:        eventTS.UTC(),
			Level:     level,
			EventType: eventType,
			Message:   message,
			Details:   cloneAlertDetailsMap(event.Details),
		})
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE worker_client
		SET last_seen_at = $2, updated_at = $2
		WHERE id = $1
	`, workerID, now)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	for _, event := range alertEvents {
		s.emitWorkerAlert(event)
	}
	return nil
}

func (s *Store) StopWorkerSession(ctx context.Context, workerID string, token string, reason string) error {
	workerID = strings.TrimSpace(workerID)
	token = strings.TrimSpace(token)
	if workerID == "" || token == "" {
		return errWorkerSessionInvalid
	}

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE worker_client
		SET
			state = $3,
			status_reason = $4,
			stopped_at = $5,
			last_seen_at = $5,
			updated_at = $5
		WHERE id = $1 AND session_token = $2
	`, workerID, token, types.WorkerStateStopped, nullableStringVal(reason), now)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errWorkerSessionInvalid
	}

	stopDetails := map[string]any{
		"reason": strings.TrimSpace(reason),
	}
	if err := s.insertWorkerEvent(ctx, workerID, now, "INFO", "worker.stopped", "Worker session stopped", stopDetails); err != nil {
		return err
	}
	s.emitWorkerAlert(WorkerAlertEvent{
		WorkerID:  workerID,
		TS:        now.UTC(),
		Level:     "INFO",
		EventType: "worker.stopped",
		Message:   "Worker session stopped",
		Details:   cloneAlertDetailsMap(stopDetails),
	})
	return nil
}

func (s *Store) ListWorkers(ctx context.Context, req types.WorkerListRequest) ([]types.WorkerStatusResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`
		SELECT
			wc.id,
			wc.application_id,
			a.name AS application_name,
			wc.app_runtime_id,
			wc.worker_name,
			wc.instance_id,
			wc.worker_version,
			wc.sdk_version,
			wc.environment,
			wc.host_name,
			wc.pid,
			wc.state,
			wc.status_reason,
			wc.broker_type,
			wc.broker_connected,
			wc.in_flight_jobs,
			wc.jobs_processed,
			wc.jobs_failed,
			wc.queue_lag,
			wc.cpu_percent,
			wc.memory_mb,
			wc.last_error,
			wc.started_at,
			wc.last_seen_at,
			wc.stopped_at,
			wc.updated_at,
			wc.supported_handlers_json,
			wc.capabilities_json,
			wc.metadata_json
		FROM worker_client wc
		JOIN application a ON a.id = wc.application_id
		WHERE 1 = 1
	`)

	args := make([]any, 0, 4)
	if req.ApplicationID != nil && *req.ApplicationID > 0 {
		args = append(args, *req.ApplicationID)
		queryBuilder.WriteString(fmt.Sprintf(" AND wc.application_id = $%d", len(args)))
	}
	if req.State != nil && strings.TrimSpace(*req.State) != "" {
		args = append(args, strings.TrimSpace(*req.State))
		queryBuilder.WriteString(fmt.Sprintf(" AND wc.state = $%d", len(args)))
	}
	if req.Search != nil && strings.TrimSpace(*req.Search) != "" {
		search := "%" + strings.ToLower(strings.TrimSpace(*req.Search)) + "%"
		args = append(args, search)
		queryBuilder.WriteString(fmt.Sprintf(
			" AND (LOWER(wc.worker_name) LIKE $%d OR LOWER(wc.instance_id) LIKE $%d OR LOWER(COALESCE(wc.host_name, '')) LIKE $%d)",
			len(args), len(args), len(args),
		))
	}

	args = append(args, limit)
	queryBuilder.WriteString(fmt.Sprintf(" ORDER BY wc.last_seen_at DESC LIMIT $%d", len(args)))

	rows := []workerClientSnapshot{}
	if err := s.db.SelectContext(ctx, &rows, queryBuilder.String(), args...); err != nil {
		return nil, err
	}

	result := make([]types.WorkerStatusResponse, 0, len(rows))
	for _, row := range rows {
		item, err := toWorkerStatusResponse(row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	return result, nil
}

func (s *Store) ListWorkerEvents(ctx context.Context, req types.WorkerEventListRequest) ([]types.WorkerEventResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`
		SELECT
			we.id,
			we.worker_id,
			wc.worker_name,
			wc.application_id,
			a.name AS application_name,
			we.ts,
			we.level,
			we.event_type,
			we.message,
			we.details_json
		FROM worker_event we
		JOIN worker_client wc ON wc.id = we.worker_id
		JOIN application a ON a.id = wc.application_id
		WHERE 1 = 1
	`)

	args := make([]any, 0, 3)
	if req.WorkerID != nil && strings.TrimSpace(*req.WorkerID) != "" {
		args = append(args, strings.TrimSpace(*req.WorkerID))
		queryBuilder.WriteString(fmt.Sprintf(" AND we.worker_id = $%d", len(args)))
	}
	if req.ApplicationID != nil && *req.ApplicationID > 0 {
		args = append(args, *req.ApplicationID)
		queryBuilder.WriteString(fmt.Sprintf(" AND wc.application_id = $%d", len(args)))
	}
	args = append(args, limit)
	queryBuilder.WriteString(fmt.Sprintf(" ORDER BY we.ts DESC LIMIT $%d", len(args)))

	type workerEventRow struct {
		ID              int64     `db:"id"`
		WorkerID        string    `db:"worker_id"`
		WorkerName      string    `db:"worker_name"`
		ApplicationID   int       `db:"application_id"`
		ApplicationName string    `db:"application_name"`
		TS              time.Time `db:"ts"`
		Level           string    `db:"level"`
		EventType       string    `db:"event_type"`
		Message         string    `db:"message"`
		DetailsJSON     string    `db:"details_json"`
	}

	rows := []workerEventRow{}
	if err := s.db.SelectContext(ctx, &rows, queryBuilder.String(), args...); err != nil {
		return nil, err
	}

	result := make([]types.WorkerEventResponse, 0, len(rows))
	for _, row := range rows {
		details := map[string]any{}
		_ = json.Unmarshal([]byte(strings.TrimSpace(row.DetailsJSON)), &details)
		if len(details) == 0 {
			details = nil
		}

		result = append(result, types.WorkerEventResponse{
			ID:              row.ID,
			WorkerID:        row.WorkerID,
			WorkerName:      row.WorkerName,
			ApplicationID:   row.ApplicationID,
			ApplicationName: row.ApplicationName,
			TS:              row.TS.UTC().Format(time.RFC3339),
			Level:           normalizeLogLevel(row.Level),
			EventType:       row.EventType,
			Message:         row.Message,
			Details:         details,
		})
	}

	return result, nil
}

func (s *Store) insertWorkerEvent(
	ctx context.Context,
	workerID string,
	ts time.Time,
	level string,
	eventType string,
	message string,
	details map[string]any,
) error {
	detailsJSON, err := toJSONText(details, "{}")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO worker_event (worker_id, ts, level, event_type, message, details_json)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, workerID, ts.UTC(), normalizeLogLevel(level), strings.TrimSpace(eventType), strings.TrimSpace(message), detailsJSON)
	return err
}

func insertWorkerEventTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workerID string,
	ts time.Time,
	level string,
	eventType string,
	message string,
	details map[string]any,
) error {
	detailsJSON, err := toJSONText(details, "{}")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO worker_event (worker_id, ts, level, event_type, message, details_json)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, workerID, ts.UTC(), normalizeLogLevel(level), strings.TrimSpace(eventType), strings.TrimSpace(message), detailsJSON)
	return err
}

func sanitizeWorkerState(state string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case types.WorkerStateStarting:
		return types.WorkerStateStarting
	case types.WorkerStateReady:
		return types.WorkerStateReady
	case types.WorkerStateDegraded:
		return types.WorkerStateDegraded
	case types.WorkerStateDraining:
		return types.WorkerStateDraining
	case types.WorkerStateStopped:
		return types.WorkerStateStopped
	case types.WorkerStateError:
		return types.WorkerStateError
	case types.WorkerStateOffline:
		return types.WorkerStateOffline
	default:
		if strings.TrimSpace(fallback) == "" {
			return types.WorkerStateStarting
		}
		return fallback
	}
}

func normalizeLogLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "TRACE":
		return "TRACE"
	case "DEBUG":
		return "DEBUG"
	case "WARN", "WARNING":
		return "WARN"
	case "ERROR":
		return "ERROR"
	default:
		return "INFO"
	}
}

func toWorkerStatusResponse(row workerClientSnapshot) (types.WorkerStatusResponse, error) {
	supportedHandlers := []string{}
	if strings.TrimSpace(row.SupportedJSON) != "" {
		_ = json.Unmarshal([]byte(row.SupportedJSON), &supportedHandlers)
	}

	capabilities := map[string]any{}
	if strings.TrimSpace(row.CapabilitiesJSON) != "" {
		_ = json.Unmarshal([]byte(row.CapabilitiesJSON), &capabilities)
	}
	if len(capabilities) == 0 {
		capabilities = nil
	}

	metadata := map[string]any{}
	if strings.TrimSpace(row.MetadataJSON) != "" {
		_ = json.Unmarshal([]byte(row.MetadataJSON), &metadata)
	}
	if len(metadata) == 0 {
		metadata = nil
	}

	resp := types.WorkerStatusResponse{
		ID:                row.ID,
		ApplicationID:     row.ApplicationID,
		ApplicationName:   row.ApplicationName,
		AppID:             row.AppRuntimeID,
		WorkerName:        row.WorkerName,
		InstanceID:        row.InstanceID,
		State:             row.State,
		EffectiveState:    row.State,
		BrokerConnected:   row.BrokerConnected,
		InFlightJobs:      row.InFlightJobs,
		JobsProcessed:     row.JobsProcessed,
		JobsFailed:        row.JobsFailed,
		StartedAt:         row.StartedAt.UTC().Format(time.RFC3339),
		LastSeenAt:        row.LastSeenAt.UTC().Format(time.RFC3339),
		UpdatedAt:         row.UpdatedAt.UTC().Format(time.RFC3339),
		SupportedHandlers: supportedHandlers,
		Capabilities:      capabilities,
		Metadata:          metadata,
	}
	if row.WorkerVersion.Valid {
		value := row.WorkerVersion.String
		resp.WorkerVersion = &value
	}
	if row.SDKVersion.Valid {
		value := row.SDKVersion.String
		resp.SDKVersion = &value
	}
	if row.Environment.Valid {
		value := row.Environment.String
		resp.Environment = &value
	}
	if row.HostName.Valid {
		value := row.HostName.String
		resp.HostName = &value
	}
	if row.PID.Valid {
		value := int(row.PID.Int32)
		resp.PID = &value
	}
	if row.StatusReason.Valid {
		value := row.StatusReason.String
		resp.StatusReason = &value
	}
	if row.BrokerType.Valid {
		value := row.BrokerType.String
		resp.BrokerType = &value
	}
	if row.QueueLag.Valid {
		value := int(row.QueueLag.Int32)
		resp.QueueLag = &value
	}
	if row.CPUPercent.Valid {
		value := row.CPUPercent.Float64
		resp.CPUPercent = &value
	}
	if row.MemoryMB.Valid {
		value := row.MemoryMB.Float64
		resp.MemoryMB = &value
	}
	if row.LastError.Valid {
		value := row.LastError.String
		resp.LastError = &value
	}
	if row.StoppedAt.Valid {
		value := row.StoppedAt.Time.UTC().Format(time.RFC3339)
		resp.StoppedAt = &value
	}

	return resp, nil
}

func toJSONText(value any, fallback string) (string, error) {
	if value == nil {
		return fallback, nil
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(bytes))
	if text == "" || text == "null" {
		return fallback, nil
	}
	return text, nil
}

func nullableStringVal(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func maxInt(value int, minValue int) int {
	if value < minValue {
		return minValue
	}
	return value
}

func maxInt64(value int64, minValue int64) int64 {
	if value < minValue {
		return minValue
	}
	return value
}
