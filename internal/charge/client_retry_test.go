package charge

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type countLimiter struct {
	mu    sync.Mutex
	waits int
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func (l *countLimiter) Wait(context.Context) error {
	l.mu.Lock()
	l.waits++
	l.mu.Unlock()
	return nil
}

func (l *countLimiter) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.waits
}

func TestRetryRecreatesPOSTBodyAndConsumesLimiterPerAttempt(t *testing.T) {
	requests := 0
	var bodies []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		status := http.StatusOK
		responseBody := `{"code":200,"map":{"surplusCharge":"12.5"}}`
		if requests < 3 {
			status = http.StatusServiceUnavailable
			responseBody = "temporary"
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    r,
		}, nil
	})

	limiter := &countLimiter{}
	client := NewClientWithLimiter("https://example.test", "token", limiter)
	client.httpClient.Transport = transport
	client.retryBackoff = func(int) time.Duration { return 0 }
	data := url.Values{"room": {"101"}, "building": {"A"}}
	result, err := client.thirdContext(context.Background(), 409, data)
	if err != nil {
		t.Fatal(err)
	}
	if result["surplusCharge"] != "12.5" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if requests != 3 || limiter.Count() != 3 {
		t.Fatalf("requests=%d limiter waits=%d, want 3/3", requests, limiter.Count())
	}
	for i, body := range bodies {
		if body != data.Encode() {
			t.Fatalf("body %d = %q, want %q", i, body, data.Encode())
		}
	}
}

func TestResponseCodeRejectsFractionalNumber(t *testing.T) {
	if _, ok := responseCode(200.5); ok {
		t.Fatal("fractional response code should be rejected")
	}
}

func TestResponseCodeRejectsStringWithTrailingGarbage(t *testing.T) {
	if _, ok := responseCode("200x"); ok {
		t.Fatal("response code with trailing garbage should be rejected")
	}
}

func TestRedirectConsumesLimiterSlot(t *testing.T) {
	limiter := &countLimiter{}
	client := NewClientWithLimiter("https://example.test", "token", limiter)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/start" {
			header := make(http.Header)
			header.Set("Location", "/done")
			return &http.Response{StatusCode: http.StatusFound, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
	})
	response, err := client.do(context.Background(), http.MethodGet, "/start", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := limiter.Count(); got != 2 {
		t.Fatalf("limiter waits = %d, want 2 for initial request and redirect", got)
	}
}

func TestCrossOriginRedirectIsRejectedWithoutRetry(t *testing.T) {
	limiter := &countLimiter{}
	requests := 0
	client := NewClientWithLimiter("https://example.test", "token", limiter)
	client.retryBackoff = func(int) time.Duration { return 0 }
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		header := make(http.Header)
		header.Set("Location", "https://other.test/done")
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})
	if _, err := client.do(context.Background(), http.MethodGet, "/start", nil, nil); err == nil {
		t.Fatal("cross-origin redirect should fail")
	}
	if requests != 1 || limiter.Count() != 1 {
		t.Fatalf("requests=%d limiter waits=%d, want 1/1", requests, limiter.Count())
	}
}

func TestSameOriginTreatsDefaultPortAsEquivalent(t *testing.T) {
	withoutPort, err := url.Parse("https://example.test/start")
	if err != nil {
		t.Fatal(err)
	}
	withPort, err := url.Parse("https://EXAMPLE.test:443/done")
	if err != nil {
		t.Fatal(err)
	}
	if !sameOrigin(withoutPort, withPort) {
		t.Fatal("default HTTPS port should be same-origin")
	}
}
