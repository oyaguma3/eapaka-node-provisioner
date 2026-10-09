package api

import (
	"encoding/json/v2"
	"net/http"
)

// ProblemDetails の cause の値（docs/openapi/provisioner-api.yaml の ProblemDetails）。
const (
	causeOptionalIEIncorrect = "OPTIONAL_IE_INCORRECT"
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.MarshalWrite(w, v)
}
