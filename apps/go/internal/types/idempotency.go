package types

// IdempotentPipelineCreateRequest is the opt-in, fail-safe pipeline creation
// contract. Authentication is supplied through X-API-Key; ApiKey in the
// embedded legacy request is accepted for wire compatibility but ignored.
type IdempotentPipelineCreateRequest struct {
	PipelineCreateRequest
	IdempotencyKey string `json:"idempotencyKey"`
}

// IdempotentPipelineCreateResponse reports whether this request created the
// pipeline or resolved an existing pipeline for the same application-scoped
// idempotency key.
type IdempotentPipelineCreateResponse struct {
	Pipeline    *PipelineResponse `json:"pipeline"`
	Created     bool              `json:"created"`
	WasExisting bool              `json:"wasExisting"`
}

// PipelineIdempotencyLookupRequest identifies a pipeline without placing the
// idempotency key in the request URL or access logs.
type PipelineIdempotencyLookupRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
}
