package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func spaHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		f, err := root.Open(path[1:])
		if err != nil {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}

// findFile walks the embedded FS and returns the first file matching the given
// glob pattern (relative to root). Returns "" if none found.
func findFile(root fs.FS, pattern string) string {
	var found string
	fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return err
		}
		if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			found = path
		}
		return nil
	})
	return found
}

func TestSPAHandler(t *testing.T) {
	root, err := DistFS()
	if err != nil {
		t.Fatal(err)
	}
	handler := spaHandler(root)

	// Discover real filenames from the build output so tests survive rebuilds.
	jsChunk := findFile(root, "Cg5Z0sVc.js") // small chunk that's likely stable
	if jsChunk == "" {
		// Fall back: just pick any .js in chunks/
		jsChunk = findFile(root, "*.js")
	}
	cssFile := findFile(root, "*.css")
	startJS := findFile(root, "start.*.js")

	tests := []struct {
		path           string
		wantStatus     int
		wantMIME       string
		wantBodySubstr string
	}{
		{"/", 200, "text/html", "<!DOCTYPE html>"},
		// /index.html → 301 redirect to / (standard http.FileServer behavior)
		{"/index.html", 301, "", ""},
		{"/favicon.svg", 200, "image/svg+xml", "<svg"},
		{"/_app/env.js", 200, "text/javascript", ""},
		// SPA fallback for non-existent paths
		{"/some-slug", 200, "text/html", "<!DOCTYPE html>"},
		{"/some/deep/path", 200, "text/html", "<!DOCTYPE html>"},
		// Edge cases: directory paths (http.FileServer redirects dirs without trailing slash)
		{"/_app", 301, "", ""},
		{"/_app/immutable", 301, "", ""},
	}

	// Add dynamically-discovered asset paths.
	if jsChunk != "" {
		tests = append(tests, struct {
			path           string
			wantStatus     int
			wantMIME       string
			wantBodySubstr string
		}{"/" + jsChunk, 200, "text/javascript", ""})
	}
	if cssFile != "" {
		tests = append(tests, struct {
			path           string
			wantStatus     int
			wantMIME       string
			wantBodySubstr string
		}{"/" + cssFile, 200, "text/css", ""})
	}
	if startJS != "" {
		tests = append(tests, struct {
			path           string
			wantStatus     int
			wantMIME       string
			wantBodySubstr string
		}{"/" + startJS, 200, "text/javascript", ""})
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("path=%s: status=%d, want %d", tt.path, rec.Code, tt.wantStatus)
			}

			ct := rec.Header().Get("Content-Type")
			if tt.wantMIME != "" && !strings.Contains(ct, tt.wantMIME) {
				t.Errorf("path=%s: Content-Type=%q, want containing %q", tt.path, ct, tt.wantMIME)
			}

			body := rec.Body.String()
			if tt.wantBodySubstr != "" && !strings.Contains(body, tt.wantBodySubstr) {
				t.Errorf("path=%s: body doesn't contain %q (got %d bytes)", tt.path, tt.wantBodySubstr, len(body))
			}

			t.Logf("path=%-50s status=%d Content-Type=%s size=%d", tt.path, rec.Code, ct, rec.Body.Len())
		})
	}
}

func TestSPAHandlerBehindMux(t *testing.T) {
	root, err := DistFS()
	if err != nil {
		t.Fatal(err)
	}

	// Discover real filenames from the build output.
	jsChunk := findFile(root, "*.js")
	startJS := findFile(root, "start.*.js")

	mux := http.NewServeMux()
	mux.HandleFunc("/h/{slug}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("capture"))
	})
	mux.HandleFunc("/h/{slug}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("capture_rest"))
	})
	mux.HandleFunc("/ws/{slug}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ws"))
	})
	mux.HandleFunc("POST /api/endpoints", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("api"))
	})
	mux.Handle("/", spaHandler(root))

	tests := []struct {
		method     string
		path       string
		wantStatus int
		wantMIME   string
		wantBody   string
	}{
		// Static assets — must serve with correct MIME types (dynamically discovered)
		{"GET", "/_app/env.js", 200, "text/javascript", ""},
		{"GET", "/favicon.svg", 200, "image/svg+xml", ""},
		// Root serves index.html
		{"GET", "/", 200, "text/html", ""},
		// SPA client routes — fallback to index.html
		{"GET", "/my-endpoint-slug", 200, "text/html", ""},
		// Capture routes — must NOT be intercepted by SPA
		{"POST", "/h/abc123", 200, "", "capture"},
		{"GET", "/h/abc123", 200, "", "capture"},
		{"POST", "/h/abc123/subpath", 200, "", "capture_rest"},
		// WebSocket route
		{"GET", "/ws/abc123", 200, "", "ws"},
	}

	// Add dynamically-discovered asset paths.
	if jsChunk != "" {
		tests = append(tests, struct {
			method     string
			path       string
			wantStatus int
			wantMIME   string
			wantBody   string
		}{"GET", "/" + jsChunk, 200, "text/javascript", ""})
	}
	if startJS != "" {
		tests = append(tests, struct {
			method     string
			path       string
			wantStatus int
			wantMIME   string
			wantBody   string
		}{"GET", "/" + startJS, 200, "text/javascript", ""})
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status=%d, want %d", rec.Code, tt.wantStatus)
			}
			ct := rec.Header().Get("Content-Type")
			if tt.wantMIME != "" && !strings.Contains(ct, tt.wantMIME) {
				t.Errorf("Content-Type=%q, want containing %q", ct, tt.wantMIME)
			}
			body := rec.Body.String()
			if tt.wantBody != "" && body != tt.wantBody {
				t.Errorf("body=%q, want %q", body, tt.wantBody)
			}
			t.Logf("%-6s %-55s → status=%d type=%s", tt.method, tt.path, rec.Code, ct)
		})
	}
}
