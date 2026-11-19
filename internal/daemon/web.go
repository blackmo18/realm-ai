package daemon

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/ids"
	"github.com/blackmo18/realm-ai/internal/webui"
)

// Web serves the web UI and the public API on 127.0.0.1 with a per-launch
// token. It never exposes the bot tool endpoint, shutdown or UI start.
type Web struct {
	e      *engine.Engine
	static fs.FS

	mu    sync.Mutex
	srv   *http.Server
	url   string
	token string
	port  string
}

// Start launches the listener once and returns the URL to open, which
// carries the token in its fragment (never sent to the server in requests).
func (w *Web) Start() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.srv != nil {
		return w.url, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("web listener: %w", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	w.port, w.token = port, ids.Token()
	if w.static == nil {
		w.static = webui.FS()
	}
	w.srv = &http.Server{Handler: w.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = w.srv.Serve(ln) }()
	w.url = fmt.Sprintf("http://127.0.0.1:%s/#token=%s", port, w.token)
	return w.url, nil
}

// Close stops the listener.
func (w *Web) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.srv != nil {
		_ = w.srv.Close()
		w.srv = nil
	}
}

func (w *Web) handler() http.Handler {
	api := http.NewServeMux()
	routes(api, w.e)
	files := http.FileServerFS(w.static)
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if err := w.checkOrigin(r); err != nil {
			fail(rw, http.StatusForbidden, err)
			return
		}
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		rw.Header().Set("Referrer-Policy", "no-referrer")
		if rest, isAPI := strings.CutPrefix(r.URL.Path, "/api"); isAPI {
			if !w.authorized(r) {
				fail(rw, http.StatusUnauthorized, errors.New("missing or wrong token; open the URL printed by `realm ui`"))
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = rest
			api.ServeHTTP(rw, r2)
			return
		}
		rw.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; connect-src 'self'; frame-ancestors 'none'")
		if _, err := fs.Stat(w.static, strings.TrimPrefix(r.URL.Path, "/")); err != nil || r.URL.Path == "/" {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			files.ServeHTTP(rw, r2) // single-page app: unknown paths get index.html
			return
		}
		files.ServeHTTP(rw, r)
	})
}

// checkOrigin blocks DNS rebinding (Host) and cross-site requests (Origin).
func (w *Web) checkOrigin(r *http.Request) error {
	allowed := []string{"127.0.0.1:" + w.port, "localhost:" + w.port}
	hostOK := false
	for _, h := range allowed {
		hostOK = hostOK || r.Host == h
	}
	if !hostOK {
		return fmt.Errorf("host %q not allowed", r.Host)
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		for _, h := range allowed {
			if origin == "http://"+h {
				return nil
			}
		}
		return fmt.Errorf("origin %q not allowed", origin)
	}
	return nil
}

func (w *Web) authorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" && r.URL.Path == "/api/v1/stream" {
		got = r.URL.Query().Get("token") // EventSource cannot set headers
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(w.token)) == 1
}
