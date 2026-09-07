package api

// externalTestSchema is the SQLite schema shared by the API test suites. It mirrors the
// Liquibase changelog closely enough to exercise the real store queries.
const externalTestSchema = `
	CREATE TABLE pipeline (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		status TEXT,
		created_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP NULL,
		is_completed BOOLEAN NOT NULL DEFAULT 0,
		application_id INTEGER NULL,
		trace_id TEXT NULL,
		idempotency_key TEXT NULL,
		request_hash TEXT NULL,
		schedule_id INTEGER NULL,
		schedule_run_id INTEGER NULL
	);
	CREATE UNIQUE INDEX uq_pipeline_application_idempotency_key
		ON pipeline(application_id, idempotency_key);
	CREATE TABLE stage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		stage_handler_name TEXT NULL,
		description TEXT NULL,
		status TEXT NULL,
		pipeline_id INTEGER NOT NULL,
		created_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP NULL,
		started_at TIMESTAMP NULL,
		is_skipped BOOLEAN NULL,
		is_event BOOLEAN NULL,
		span_id TEXT NULL,
		retry_attempt INTEGER NOT NULL DEFAULT 0,
		next_retry_at TIMESTAMP NULL,
		approval_decision BOOLEAN NULL,
		approval_rejection_reason TEXT NULL,
		approval_resumed_at TIMESTAMP NULL,
		approval_resumed_by TEXT NULL,
		execution_id TEXT NULL,
		execution_attempt INTEGER NOT NULL DEFAULT 0,
		dispatched_at TIMESTAMP NULL,
		lease_owner TEXT NULL,
		lease_expires_at TIMESTAMP NULL,
		last_error_code TEXT NULL,
		failure_disposition TEXT NULL
	);
	CREATE TABLE stage_io (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		input TEXT NULL,
		output TEXT NULL,
		stage_id INTEGER NOT NULL
	);
	CREATE TABLE stage_options (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stage_id INTEGER NOT NULL,
		run_next_if_failed BOOLEAN NULL,
		retry_interval INTEGER NULL,
		time_out INTEGER NULL,
		max_retries INTEGER NULL,
		retry_on_error_codes TEXT NULL,
		retry_backoff TEXT NULL,
		max_retry_interval INTEGER NULL,
		retry_jitter BOOLEAN NULL,
		depends_on TEXT NULL,
		run_in_parallel_with TEXT NULL,
		fail_if_output_empty BOOLEAN NULL,
		notify_on_failure BOOLEAN NULL,
		run_as_user TEXT NULL
	);
	CREATE TABLE stage_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		log TEXT NULL,
		log_level TEXT NULL,
		created_at TIMESTAMP NULL,
		stage_id INTEGER NOT NULL
	);
	CREATE TABLE pipeline_context_item (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		value_type TEXT NULL,
		is_sensitive BOOLEAN NOT NULL DEFAULT 0,
		pipeline_id INTEGER NOT NULL
	);
	CREATE TABLE api_key (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		application_id INTEGER NOT NULL,
		name TEXT NULL,
		key TEXT NULL,
		key_hash TEXT NULL,
		key_prefix TEXT NULL,
		created_at TIMESTAMP NULL,
		disabled_at TIMESTAMP NULL,
		expires_at TIMESTAMP NULL,
		last_used TIMESTAMP NULL
	);
	CREATE TABLE application (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT NULL,
		created_at TIMESTAMP NULL
	);
	CREATE TABLE schedule (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		application_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'Active',
		kind TEXT NOT NULL,
		cron_expression TEXT NULL,
		time_zone TEXT NOT NULL DEFAULT 'UTC',
		interval_seconds INTEGER NULL,
		run_at TIMESTAMP NULL,
		definition_json TEXT NOT NULL DEFAULT '{}',
		definition_version INTEGER NOT NULL DEFAULT 1,
		overlap_policy TEXT NOT NULL DEFAULT 'Skip',
		catchup_policy TEXT NOT NULL DEFAULT 'None',
		catchup_max INTEGER NOT NULL DEFAULT 10,
		jitter_seconds INTEGER NOT NULL DEFAULT 0,
		next_run_at TIMESTAMP NULL,
		last_run_at TIMESTAMP NULL,
		last_run_status TEXT NULL,
		created_by TEXT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);
	CREATE UNIQUE INDEX uq_schedule_application_name ON schedule(application_id, lower(name));
	CREATE TABLE schedule_run (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		schedule_id INTEGER NOT NULL,
		scheduled_for TIMESTAMP NOT NULL,
		fired_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		trigger TEXT NOT NULL,
		outcome TEXT NOT NULL,
		skip_reason TEXT NULL,
		pipeline_id INTEGER NULL,
		definition_version INTEGER NOT NULL DEFAULT 1,
		error TEXT NULL
	);
	CREATE UNIQUE INDEX uq_schedule_run_tick ON schedule_run(schedule_id, scheduled_for);
	CREATE TABLE keyword (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL,
		value TEXT NOT NULL
	);
	CREATE TABLE pipeline_keyword (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pipeline_id INTEGER NOT NULL,
		keyword_id INTEGER NOT NULL
	);
`
