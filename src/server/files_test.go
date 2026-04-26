package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseDeletePathQuery(t *testing.T) {
	tests := []struct {
		name      string
		values    url.Values
		wantPath  string
		wantError string
	}{
		{
			name:     "file path",
			values:   url.Values{deletePathQuery: {"./archive/2026/video.mp4"}},
			wantPath: "./archive/2026/video.mp4",
		},
		{
			name:     "directory path",
			values:   url.Values{deletePathQuery: {"archive/2026/"}},
			wantPath: "archive/2026/",
		},
		{
			name:      "invalid path",
			values:    url.Values{deletePathQuery: {"./"}},
			wantError: "削除対象の path クエリが不正です",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotError := parseDeletePathQuery(tt.values)
			if gotPath != tt.wantPath {
				t.Fatalf("parseDeletePathQuery() path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotError != tt.wantError {
				t.Fatalf("parseDeletePathQuery() error = %q, want %q", gotError, tt.wantError)
			}
		})
	}
}

func TestRegisterRoutesDeleteFilesRequiresPathQuery(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "path クエリが必要です") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestRegisterRoutesDeleteFilesRejectsLegacyQuery(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files?key=sample.mp4", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "削除には path クエリを使ってください") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestRegisterRoutesDeleteFilesRejectsMixedDeleteTarget(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files?path=sample.mp4&key=legacy.mp4", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "削除対象は path クエリだけを指定してください") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}
