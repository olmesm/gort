package web

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Problem is an RFC 7807 problem+json response, carried as an error value
// from a handler to the adapter that writes it.
type Problem struct {
	Status int
	Type   string
	Title  string
	Detail string
}

func (p *Problem) Error() string { return fmt.Sprintf("%d %s: %s", p.Status, p.Title, p.Detail) }

type problemDetails struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

func writeProblem(w http.ResponseWriter, p *Problem) {
	body, _ := json.Marshal(problemDetails{
		Type:   "https://gort.dev/errors/" + p.Type,
		Title:  p.Title,
		Detail: p.Detail,
		Status: p.Status,
	})
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_, _ = w.Write(body)
}

var internalProblem = &Problem{500, "internal", "Internal server error", "Something went wrong handling the request."}

func NewProblem(status int, problemType, title, detail string) error {
	return &Problem{Status: status, Type: problemType, Title: title, Detail: detail}
}

func BadRequest(detail string) error {
	return NewProblem(400, "invalid-data", "Invalid data", detail)
}

func Unauthorized(detail string) error {
	return NewProblem(401, "missing-authentication", "Authentication required", detail)
}

func Forbidden(detail string) error {
	return NewProblem(403, "forbidden", "Forbidden", detail)
}

func NotFound(detail string) error {
	return NewProblem(404, "not-found", "Not found", detail)
}

func Conflict(problemType, detail string) error {
	return NewProblem(409, problemType, "Conflict", detail)
}
