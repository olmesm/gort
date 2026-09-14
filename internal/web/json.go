package web

import "encoding/json"

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
