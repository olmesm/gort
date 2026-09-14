package data

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

// createSQLiteFile gives new databases private permissions before SQLite opens
// them. Existing files keep the operator's permissions. Memory databases and
// URI modes that require an existing file must not create a file here.
func createSQLiteFile(dsn string) error {
	path, _, _ := strings.Cut(dsn, "?")
	if strings.HasPrefix(dsn, "file:") {
		u, err := url.Parse(dsn)
		if err != nil {
			return err
		}
		q := u.Query()
		if mode := q.Get("mode"); mode == "memory" || mode == "ro" || mode == "rw" {
			return nil
		}
		if u.Host != "" && u.Host != "localhost" {
			return nil // Leave unsupported URI authorities to SQLite's validator.
		}
		path = u.Path
		if u.Opaque != "" {
			path, err = url.PathUnescape(u.Opaque)
			if err != nil {
				return err
			}
		}
	}
	if path == "" || path == ":memory:" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}
