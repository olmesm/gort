package web

import (
	"errors"
	"net/http"
)

// handler is an http.HandlerFunc that reports failure by returning an error
// instead of writing the response itself. App.handle adapts it: a nil return
// means the handler already wrote its reply; a non-nil one is turned into the
// response by handleError, so handlers read as `if err != nil { return err }`.
type handler func(w http.ResponseWriter, r *http.Request) error

// apiHandler and userHandler are handlers that additionally receive the
// authenticated principal from the require* middleware.
type apiHandler func(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error
type userHandler func(user *CurrentUser, w http.ResponseWriter, r *http.Request) error

func (a *App) handle(fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			a.handleError(w, err)
		}
	}
}

// handleError writes the response for an error returned by a handler. A
// *Problem or *plainError is the handler's intended reply and is written as
// is; anything else is an unexpected failure, logged and answered with a
// generic 500 so internals never leak to the client.
func (a *App) handleError(w http.ResponseWriter, err error) {
	var problem *Problem
	if errors.As(err, &problem) {
		writeProblem(w, problem)
		return
	}
	var plain *plainError
	if errors.As(err, &plain) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(plain.status)
		_, _ = w.Write([]byte(plain.message))
		return
	}
	a.Logger.Error("request failed", "error", err)
	writeProblem(w, internalProblem)
}

// plainError is a text/plain reply for dashboard routes, where a JSON
// problem document would be the wrong thing to show a browser.
type plainError struct {
	status  int
	message string
}

func (e *plainError) Error() string { return e.message }

var (
	errPageNotFound = &plainError{http.StatusNotFound, "Not found"}
	errAdminOnly    = &plainError{http.StatusForbidden, "Forbidden: admin access required."}
)
