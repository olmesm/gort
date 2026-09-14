package web

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDataDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	for _, existing := range []bool{false, true} {
		dir := filepath.Join(t.TempDir(), "data")
		want := os.FileMode(0o700)
		if existing {
			want = 0o750
			if err := os.Mkdir(dir, want); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, want); err != nil {
				t.Fatal(err)
			}
		}
		newTestAppWithConfig(t, map[string]string{"DATA_DIR": dir})
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("existing=%v: expected %o, got %v, %v", existing, want, info, err)
		}
	}
}
