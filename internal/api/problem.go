package api

import (
	"encoding/json/v2"
	"net/http"
)

// ProblemDetails の cause の値（docs/openapi/provisioner-api.yaml の ProblemDetails）。
const (
	causeInvalidMsgFormat        = "INVALID_MSG_FORMAT"
	causeInvalidQueryParam       = "INVALID_QUERY_PARAM"
	causeMandatoryIEMissing      = "MANDATORY_IE_MISSING"
	causeMandatoryIEIncorrect    = "MANDATORY_IE_INCORRECT"
	causeOptionalIEIncorrect     = "OPTIONAL_IE_INCORRECT"
	causeSystemFailure           = "SYSTEM_FAILURE"
	causeUserNotFound            = "USER_NOT_FOUND"
	causeSubscriberExists        = "SUBSCRIBER_ALREADY_EXISTS"
	causeKeyStoreMismatch        = "KEY_STORE_MISMATCH"
	causeOperationIncomplete     = "OPERATION_INCOMPLETE"
	causeOperationNotFound       = "OPERATION_NOT_FOUND"
	causeOperationStateConflict  = "OPERATION_STATE_CONFLICT"
	causeOperationInProgress     = "OPERATION_IN_PROGRESS"
	causeIdempotencyKeyMismatch  = "IDEMPOTENCY_KEY_MISMATCH"
	causeDownstreamError         = "DOWNSTREAM_ERROR"
	causeDownstreamUnavailable   = "DOWNSTREAM_UNAVAILABLE"
	causeDownstreamNotConfigured = "DOWNSTREAM_NOT_CONFIGURED"
)

type invalidParam struct {
	Param  string `json:"param"`
	Reason string `json:"reason,omitempty"`
}

// problem はエラー応答（ProblemDetails）。下流の作法の項目に、provisioner の拡張項目を加える。
type problem struct {
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail,omitempty"`
	Cause         string         `json:"cause,omitempty"`
	InvalidParams []invalidParam `json:"invalidParams,omitempty"`

	// Downstream はエラーの元の下流（prov / aka）。
	Downstream string `json:"downstream,omitempty"`
	// DownstreamStatus は下流の HTTP ステータス。
	DownstreamStatus int `json:"downstreamStatus,omitzero"`
	// DownstreamCause は下流の cause。
	DownstreamCause string `json:"downstreamCause,omitempty"`
	// OperationID は操作の記録の ID（加入者の作成・変更・削除で、下流への書き込みを始めた後に失敗した場合）。
	OperationID string `json:"operationId,omitempty"`
	// RolledBack は、補償で元に戻したか（OperationID を返す場合だけ）。
	RolledBack *bool `json:"rolledBack,omitempty"`
	// Conflicts は、SUBSCRIBER_ALREADY_EXISTS で同じ IMSI のものがあった場所。
	Conflicts []string `json:"conflicts,omitempty"`
}

func newProblem(status int, cause, detail string) *problem {
	return &problem{Title: http.StatusText(status), Status: status, Cause: cause, Detail: detail}
}

func badParams(cause string, params ...invalidParam) *problem {
	p := newProblem(http.StatusBadRequest, cause, "")
	p.InvalidParams = params
	return p
}

func (p *problem) write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	json.MarshalWrite(w, p)
}

// internalError は内部エラー（Valkey のエラー等）を記録し、詳細を伏せた 500 を返す。
func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.Log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	newProblem(http.StatusInternalServerError, causeSystemFailure, "").write(w)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.MarshalWrite(w, v)
}
