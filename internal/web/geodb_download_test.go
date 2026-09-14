package web

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestGeoDBDownloadRejectsOversizedOrNonregularEntries(t *testing.T) {
	for _, header := range []*tar.Header{
		{Name: "GeoLite2-City.mmdb", Mode: 0600, Size: (256 << 20) + 1, Typeflag: tar.TypeReg},
		{Name: "GeoLite2-City.mmdb", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		t.Run(string(header.Typeflag), func(t *testing.T) {
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			// Only the header is needed: reject it before extracting any file body.
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			app := newTestApp(t)
			if err := os.WriteFile(app.Cfg.GeoDBPath(), []byte("existing database"), 0600); err != nil {
				t.Fatal(err)
			}
			app.geoClient = &http.Client{Transport: outboundTestTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive.Bytes())), Header: make(http.Header)}, nil
			})}
			ok, err := app.downloadGeoDB(t.Context())
			if err == nil || ok {
				t.Fatalf("accepted unsafe entry: ok=%v err=%v", ok, err)
			}
			got, err := os.ReadFile(app.Cfg.GeoDBPath())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "existing database" {
				t.Fatal("replaced existing database on rejected download")
			}
		})
	}
}
