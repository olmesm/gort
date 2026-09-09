package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// RespondJSON writes a JSON response. Wire format mirrors the REST spec:
// camelCase field names (via struct tags) with null fields omitted (via
// omitempty on pointer fields).
func RespondJSON(w http.ResponseWriter, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("serializing response: %w", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	return nil
}

// ReadJSON reads and deserializes a JSON request body, capped at 1 MiB.
// http.MaxBytesReader (unlike a plain LimitReader) also closes the
// connection on overrun so a huge body isn't read to the end.
func ReadJSON[T any](w http.ResponseWriter, r *http.Request) (*T, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("Invalid request body: %s", err)
	}
	if len(body) == 0 {
		return nil, errors.New("The request body cannot be empty.")
	}
	var parsed T
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("Invalid request body: %s", err)
	}
	return &parsed, nil
}

// Field is a PATCH-body field that distinguishes absent, explicit null and a
// value — mirroring Skippable<'T option> semantics: absent = leave unchanged,
// null = clear.
type Field[T any] struct {
	Present bool
	Null    bool
	Value   T
}

func (f *Field[T]) UnmarshalJSON(b []byte) error {
	f.Present = true
	if string(b) == "null" {
		f.Null = true
		return nil
	}
	return json.Unmarshal(b, &f.Value)
}

// Pick returns the field's value merged over the current one: absent keeps
// current, null clears (returns nil), value replaces.
func (f Field[T]) Pick(current *T) *T {
	if !f.Present {
		return current
	}
	if f.Null {
		return nil
	}
	v := f.Value
	return &v
}

// PickValue is Pick for non-nullable fields: absent keeps current, anything
// else (including null, which decodes to the zero value) replaces.
func (f Field[T]) PickValue(current T) T {
	if !f.Present {
		return current
	}
	return f.Value
}
