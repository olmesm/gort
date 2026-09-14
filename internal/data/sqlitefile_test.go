package data

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewSQLiteFilesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions")
	}
	for _, form := range []string{"path", "uri", "opaque"} {
		t.Run(form, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private database.db")
			dsn := path
			switch form {
			case "uri":
				dsn = (&url.URL{Scheme: "file", Path: path}).String() + "?mode=rwc"
			case "opaque":
				t.Chdir(filepath.Dir(path))
				dsn = "file:" + url.PathEscape(filepath.Base(path)) + "?mode=rwc"
			}
			db, err := Open(Sqlite, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(t.Context(), "CREATE TABLE private_data (value TEXT)"); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("database permissions: %v, %v", info, err)
			}
		})
	}
}

func TestSQLiteExistingPermissionsArePreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions")
	}
	path := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := createSQLiteFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("existing permissions changed: %v, %v", info, err)
	}
}

func TestSQLiteSpecialModesDoNotCreateFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, dsn := range []string{":memory:", "file::memory:?cache=shared", "file:memory.db?mode=memory&cache=shared"} {
		db, err := Open(Sqlite, dsn)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
	for _, mode := range []string{"ro", "rw"} {
		db, err := Open(Sqlite, "file:missing.db?mode="+mode)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.PingContext(t.Context()); err == nil {
			t.Fatalf("mode=%s unexpectedly created missing database", mode)
		}
		db.Close()
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected database files: %v, %v", entries, err)
	}
}
