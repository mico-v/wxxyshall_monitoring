package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	t.Setenv("ELEc_DIR", root)
	cfg := &config.Config{
		Username: "20260001", Port: 8080, BaseURL: config.DefaultBaseURL,
		PollIntervalMin: 60, RateLimitPerMinute: 30,
		Targets: []config.Target{{FeeItemID: 409, AppID: 34, Campus: "A", Building: "B", Room: "C", Label: "one"}},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	hub, err := config.NewHub()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(hub, filepath.Join(config.DataDir(), "test.db"), "0123456789abcdef", root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func TestConfigAPIProtectsFullConfigurationAndMutations(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	public := httptest.NewRecorder()
	handler.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if public.Code != http.StatusOK {
		t.Fatalf("public config status = %d", public.Code)
	}
	var publicBody map[string]any
	if err := json.Unmarshal(public.Body.Bytes(), &publicBody); err != nil {
		t.Fatal(err)
	}
	if _, ok := publicBody["username"]; ok {
		t.Fatal("public config leaked username")
	}
	if _, ok := publicBody["targets"]; !ok {
		t.Fatal("public config omitted targets")
	}
	invalidGet := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Authorization", "Bearer 0000000000000000")
	handler.ServeHTTP(invalidGet, req)
	if invalidGet.Code != http.StatusUnauthorized {
		t.Fatalf("invalid authenticated GET status = %d", invalidGet.Code)
	}

	unauthorized := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"rate_limit_per_minute":60}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(unauthorized, req)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized mutation status = %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"rate_limit_per_minute":60}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	handler.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized mutation status = %d body=%s", authorized.Code, authorized.Body.String())
	}
	if got := server.cfgHub.Config().RateLimitPerMinute; got != 60 {
		t.Fatalf("rate = %d, want 60", got)
	}
	if got := server.collector.Limiter().Rate(); got != 60 {
		t.Fatalf("collector rate = %d, want 60", got)
	}

	invalidRate := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"rate_limit_per_minute":0}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	handler.ServeHTTP(invalidRate, req)
	if invalidRate.Code != http.StatusBadRequest {
		t.Fatalf("explicit zero rate status = %d, want 400", invalidRate.Code)
	}
}

func TestAdminKeyAcceptedFromQueryParameter(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, httptest.NewRequest(http.MethodPost, "/api/admin/verify?key=0123456789abcdef", nil))
	if valid.Code != http.StatusOK {
		t.Fatalf("query key status = %d body=%s", valid.Code, valid.Body.String())
	}
	if got := valid.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("query key Cache-Control = %q, want no-store", got)
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/admin/verify?key=0000000000000000", nil))
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid query key status = %d", invalid.Code)
	}
}

func TestHiddenTargetExcludedFromPublicConfigAndRoomPage(t *testing.T) {
	server := newTestServer(t)
	hidden := false
	if _, err := server.cfgHub.UpdateConfig(func(cfg *config.Config) error {
		cfg.Targets = append(cfg.Targets, config.Target{
			FeeItemID: 409, AppID: 34, Campus: "X", Building: "Y", Room: "Z", Label: "hidden", ShowInWeb: &hidden,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	public := httptest.NewRecorder()
	server.Handler().ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var publicBody struct {
		Targets []config.Target `json:"targets"`
	}
	if err := json.Unmarshal(public.Body.Bytes(), &publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.Targets) != 1 {
		t.Fatalf("public targets = %d, want 1", len(publicBody.Targets))
	}

	admin := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	server.Handler().ServeHTTP(admin, req)
	var adminBody struct {
		Targets []config.Target `json:"targets"`
	}
	if err := json.Unmarshal(admin.Body.Bytes(), &adminBody); err != nil {
		t.Fatal(err)
	}
	if len(adminBody.Targets) != 2 {
		t.Fatalf("admin targets = %d, want 2", len(adminBody.Targets))
	}

	room := httptest.NewRecorder()
	server.Handler().ServeHTTP(room, httptest.NewRequest(http.MethodGet, "/room/X/Y/Z", nil))
	if !strings.Contains(room.Body.String(), "404") {
		t.Fatal("hidden room page should serve 404 content")
	}
}

func TestConfigAPIAddsTargetAndOnlyUpdatesLabelForDuplicate(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	add := func(label string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		body := fmt.Sprintf(`{"target":{"campus":"X","building":"Y","room":"Z","label":%q}}`, label)
		req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer 0123456789abcdef")
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := add("new room"); recorder.Code != http.StatusOK {
		t.Fatalf("add target status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	cfg := server.cfgHub.Config()
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(cfg.Targets))
	}
	got := cfg.Targets[1]
	if got.FeeItemID != config.DefaultFeeItemID || got.AppID != config.DefaultAppID || got.Label != "new room" {
		t.Fatalf("added target = %+v", got)
	}
	if cfg.PollIntervalMin != 60 || cfg.RateLimitPerMinute != 30 {
		t.Fatalf("unrelated config changed: poll=%d rate=%d", cfg.PollIntervalMin, cfg.RateLimitPerMinute)
	}
	if _, err := server.cfgHub.UpdateConfig(func(cfg *config.Config) error {
		cfg.Targets[1].FeeItemID = 777
		cfg.Targets[1].AppID = 88
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if recorder := add("renamed room"); recorder.Code != http.StatusOK {
		t.Fatalf("update duplicate target status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	cfg = server.cfgHub.Config()
	if len(cfg.Targets) != 2 || cfg.Targets[1].Label != "renamed room" ||
		cfg.Targets[1].FeeItemID != 777 || cfg.Targets[1].AppID != 88 {
		t.Fatalf("duplicate target did not only update label: %+v", cfg.Targets)
	}
}

func TestWebappSettingsOnlyExposeDormitoryAddition(t *testing.T) {
	data, err := readEmbeddedFile("webapp.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, required := range []string{`id="target-list"`, `id="pick-campus"`, `id="pick-building"`, `id="pick-room"`, `id="pick-add"`} {
		if !strings.Contains(html, required) {
			t.Errorf("settings UI omitted %s", required)
		}
	}
	for _, forbidden := range []string{
		`id="cfg-username"`, `id="cfg-port"`, `id="cfg-base-url"`, `id="cfg-poll"`, `id="cfg-rate"`,
		`id="pick-feeitemid"`, `id="pick-appid"`, `id="settings-save"`, `className = "target-actions"`,
	} {
		if strings.Contains(html, forbidden) {
			t.Errorf("settings UI still exposes %s", forbidden)
		}
	}
}

func TestFaviconIsServedAndReferencedByPages(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	icon := httptest.NewRecorder()
	handler.ServeHTTP(icon, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if icon.Code != http.StatusOK || icon.Header().Get("Content-Type") != "image/x-icon" {
		t.Fatalf("favicon response = %d %q", icon.Code, icon.Header().Get("Content-Type"))
	}
	if body := icon.Body.Bytes(); len(body) < 4 || string(body[:4]) != "\x00\x00\x01\x00" {
		t.Fatal("favicon response is not an ICO file")
	}

	for _, page := range []string{"webapp.html", "404.html", "offline.html"} {
		data, err := readEmbeddedFile(page)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `<link rel="icon" href="/favicon.ico"`) {
			t.Fatalf("%s does not reference /favicon.ico", page)
		}
	}
}

func TestDiscoverServesProcessCacheWithoutToken(t *testing.T) {
	server := newTestServer(t)
	cfg := server.cfgHub.Config()
	key := discoveryCacheKey{
		baseURL: cfg.BaseURL, feeItemID: config.DefaultFeeItemID, appID: config.DefaultAppID, kind: "campuses",
	}
	_, _, err := server.discovery.Get(context.Background(), key, func(context.Context) ([]charge.Option, error) {
		return []charge.Option{{Value: "A", Name: "campus A"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/campuses", nil)
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("cached discovery status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Elec-Discovery-Cache"); got != "hit" {
		t.Fatalf("cache header = %q, want hit", got)
	}
	var options []charge.Option
	if err := json.Unmarshal(recorder.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 || options[0].Value != "A" {
		t.Fatalf("options = %+v", options)
	}

	miss := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/buildings?campus=A", nil)
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	server.Handler().ServeHTTP(miss, req)
	if miss.Code != http.StatusUnauthorized {
		t.Fatalf("uncached discovery without token status = %d body=%s", miss.Code, miss.Body.String())
	}
}

func TestReadingsRejectsPartialRoomFilter(t *testing.T) {
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/readings?campus=A", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("partial room filter status = %d, want 400", recorder.Code)
	}
}

func TestCollectRejectsPartialTargetInsteadOfUsingFirstTarget(t *testing.T) {
	server := newTestServer(t)
	if err := server.cfgHub.SetToken(&config.Token{AccessToken: "token", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/collect", strings.NewReader(`{"campus":"A"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("partial collect target status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
}

func TestJSONEndpointsRejectLookalikeContentType(t *testing.T) {
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/jsonp")
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("lookalike content type status = %d, want 400", recorder.Code)
	}
}

func TestJSONEndpointsReturnPayloadTooLarge(t *testing.T) {
	server := newTestServer(t)
	body := `{"username":"` + strings.Repeat("x", maxJSONBody) + `"}`
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 413", recorder.Code)
	}
}

func TestGzipRemovesContentLengthAndPreservesBody(t *testing.T) {
	handler := gzipHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "11")
		_, _ = w.Write([]byte("hello world"))
	}))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br, gzip;q=0.5")
	handler.ServeHTTP(recorder, req)

	if got := recorder.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q, want empty", got)
	}
	if got := recorder.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello world" {
		t.Fatalf("body = %q", body)
	}
}

func TestGzipSkipsNoContentAndQZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
		status int
	}{
		{name: "q zero", header: "gzip;q=0", status: http.StatusOK},
		{name: "no content", header: "gzip", status: http.StatusNoContent},
		{name: "reset content", header: "gzip", status: http.StatusResetContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := gzipHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept-Encoding", tc.header)
			handler.ServeHTTP(recorder, req)
			if got := recorder.Header().Get("Content-Encoding"); got != "" {
				t.Fatalf("Content-Encoding = %q, want empty", got)
			}
			if recorder.Body.Len() != 0 {
				t.Fatalf("unexpected body bytes: %d", recorder.Body.Len())
			}
		})
	}
}

func TestAcceptsEncodingPrefersExplicitSetting(t *testing.T) {
	if acceptsEncoding("*;q=1, gzip;q=0", "gzip") {
		t.Fatal("explicit gzip;q=0 must override wildcard")
	}
	if !acceptsEncoding("br, *;q=0.5", "gzip") {
		t.Fatal("positive wildcard should allow gzip")
	}
}

func TestGzipSkipsAlreadyCompressedMedia(t *testing.T) {
	handler := gzipHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "8")
		_, _ = w.Write([]byte("png-data"))
	}))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/icon.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	handler.ServeHTTP(recorder, req)
	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if got := recorder.Header().Get("Content-Length"); got != "8" {
		t.Fatalf("Content-Length = %q, want 8", got)
	}
	if got := recorder.Body.String(); got != "png-data" {
		t.Fatalf("body = %q", got)
	}
}

func TestJobManagerCancellationLifecycle(t *testing.T) {
	manager := NewJobManager()
	job, ctx, ok := manager.Start(context.Background(), 2)
	if !ok || job == nil || ctx == nil {
		t.Fatal("failed to start job")
	}
	if manager.Cancel("wrong-id") {
		t.Fatal("wrong job ID cancelled active job")
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("wrong job ID changed context: %v", err)
	}
	if !manager.Cancel(job.ID) {
		t.Fatal("Cancel returned false")
	}
	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("job context error = %v", err)
	}
	if got := manager.Get(job.ID); got == nil || got.State != JobStateCancelling {
		t.Fatalf("job state after cancel = %+v", got)
	}
	if _, _, ok := manager.Start(context.Background(), 1); ok {
		t.Fatal("new job started before cancelled goroutine finished")
	}
	manager.Update(job.ID, func(job *CollectJob) { job.State = JobStateCancelled })
	manager.FinishActive(job.ID)
	if _, _, ok := manager.Start(context.Background(), 1); !ok {
		t.Fatal("new job did not start after FinishActive")
	}
}

func TestPWAAssetsAreEmbeddedAndConsistent(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, httptest.NewRequest(http.MethodGet, "/manifest.json", nil))
	if manifestRecorder.Code != http.StatusOK {
		t.Fatalf("manifest status = %d", manifestRecorder.Code)
	}
	var manifest struct {
		ID    string `json:"id"`
		Scope string `json:"scope"`
		Icons []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Type    string `json:"type"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(manifestRecorder.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Icons) != 2 {
		t.Fatalf("manifest icon count = %d", len(manifest.Icons))
	}
	if manifest.ID != "/" || manifest.Scope != "/" {
		t.Fatalf("manifest id/scope = %q/%q", manifest.ID, manifest.Scope)
	}
	expectedSizes := map[string][2]int{
		"/static/icon-192.png": {192, 192},
		"/static/icon-512.png": {512, 512},
	}
	for _, icon := range manifest.Icons {
		if icon.Type != "image/png" {
			t.Fatalf("icon type = %q", icon.Type)
		}
		if !strings.Contains(icon.Purpose, "maskable") {
			t.Fatalf("icon purpose = %q, want maskable", icon.Purpose)
		}
		expected, ok := expectedSizes[icon.Src]
		if !ok || icon.Sizes != fmt.Sprintf("%dx%d", expected[0], expected[1]) {
			t.Fatalf("icon manifest entry invalid: %+v", icon)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, icon.Src, nil))
		if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "image/png") {
			t.Fatalf("icon %s response = %d %q", icon.Src, recorder.Code, recorder.Header().Get("Content-Type"))
		}
		if body := recorder.Body.Bytes(); len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
			t.Fatalf("icon %s is not PNG", icon.Src)
		}
		decoded, err := png.DecodeConfig(bytes.NewReader(recorder.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Width != expected[0] || decoded.Height != expected[1] {
			t.Fatalf("icon %s dimensions = %dx%d", icon.Src, decoded.Width, decoded.Height)
		}
	}

	swRecorder := httptest.NewRecorder()
	handler.ServeHTTP(swRecorder, httptest.NewRequest(http.MethodGet, "/sw.js", nil))
	if swRecorder.Code != http.StatusOK || !strings.Contains(swRecorder.Body.String(), "`${CACHE_PREFIX}v8`") {
		t.Fatalf("service worker response invalid: status=%d", swRecorder.Code)
	}
	if got := swRecorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("service worker Cache-Control = %q, want no-cache", got)
	}
}
