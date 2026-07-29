package jobs

import "encoding/json"

type IdempotencyDecision struct {
	Replay bool
	Status int
	Body   json.RawMessage
	Error  APIError
}

func ResolveIdempotency(record IdempotencyRecord, found bool, requestHash string) IdempotencyDecision {
	if !found {
		return IdempotencyDecision{}
	}
	if record.RequestHash != requestHash {
		return IdempotencyDecision{
			Error: APIError{
				Code:      CodeInvalidRequest,
				Message:   "Idempotency-Key was already used with a different request body.",
				Retryable: false,
				Status:    409,
			},
		}
	}
	if record.ResponseStatus > 0 {
		return IdempotencyDecision{
			Replay: true,
			Status: record.ResponseStatus,
			Body:   record.ResponseMetadata,
		}
	}
	return IdempotencyDecision{}
}
