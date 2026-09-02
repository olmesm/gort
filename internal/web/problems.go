package web

import (
	"encoding/json"
	"net/http"
)

// RFC 7807 problem+json error responses.

type ProblemDetails struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

func Problem(w http.ResponseWriter, status int, problemType, title, detail string) {
	body, _ := json.Marshal(ProblemDetails{
		Type:   "https://gort.dev/errors/" + problemType,
		Title:  title,
		Detail: detail,
		Status: status,
	})
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func BadRequest(w http.ResponseWriter, detail string) {
	Problem(w, 400, "invalid-data", "Invalid data", detail)
}

func Unauthorized(w http.ResponseWriter, detail string) {
	Problem(w, 401, "missing-authentication", "Authentication required", detail)
}

func Forbidden(w http.ResponseWriter, detail string) {
	Problem(w, 403, "forbidden", "Forbidden", detail)
}

func NotFound(w http.ResponseWriter, detail string) {
	Problem(w, 404, "not-found", "Not found", detail)
}

func Conflict(w http.ResponseWriter, problemType, detail string) {
	Problem(w, 409, problemType, "Conflict", detail)
}
