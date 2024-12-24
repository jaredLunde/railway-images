package railwayimages

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		opt     Options
		wantErr bool
	}{
		{
			name:    "empty URL",
			opt:     Options{},
			wantErr: true,
		},
		{
			name: "invalid URL",
			opt: Options{
				URL: "://invalid",
			},
			wantErr: true,
		},
		{
			name: "valid URL",
			opt: Options{
				URL: "http://example.com",
			},
			wantErr: false,
		},
		{
			name: "valid URL with secret key",
			opt: Options{
				URL:       "http://example.com",
				SecretKey: "secret",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.opt)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewClient() returned nil client without error")
			}
		})
	}
}

func TestSigningTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key := r.Header.Get("x-api-key"); subtle.ConstantTimeCompare([]byte(key), []byte("test-secret")) != 1 {
			t.Errorf("expected x-api-key header to be test-secret, got %s", key)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &SigningTransport{
		transport: http.DefaultTransport,
		SecretKey: "test-secret",
	}

	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
}

func TestClient_Sign(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}
		if r.URL.Path != "/sign/test.jpg" {
			t.Errorf("expected path /sign/test.jpg, got %s", r.URL.Path)
		}
		w.Write([]byte("signed-url"))
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	client := &Client{
		URL:       serverURL,
		transport: http.DefaultTransport,
	}

	signedURL, err := client.Sign("/test.jpg")
	if err != nil {
		t.Fatal(err)
	}

	if signedURL != "signed-url" {
		t.Errorf("expected signed-url, got %s", signedURL)
	}
}

func TestClient_Sign_Local(t *testing.T) {
	tests := []struct {
		name               string
		path               string
		signatureSecretKey string
		wantErr            bool
	}{
		{
			name:               "successful local signing",
			path:               "/files/test.jpg",
			signatureSecretKey: "secret",
			wantErr:            false,
		},
		{
			name:               "empty path",
			path:               "",
			signatureSecretKey: "secret",
			wantErr:            true,
		},
		{
			name:               "path with query params",
			path:               "/serve/files/test.jpg",
			signatureSecretKey: "secret",
			wantErr:            false,
		},
		{
			name:               "invalid path",
			path:               "/test.jpg",
			signatureSecretKey: "secret",
			wantErr:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, err := url.Parse("http://example.com")
			if err != nil {
				t.Fatalf("failed to parse base URL: %v", err)
			}

			client := &Client{
				URL:                baseURL,
				SignatureSecretKey: tt.signatureSecretKey,
				transport:          http.DefaultTransport,
			}

			signedURL, err := client.Sign(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.Sign() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify the signed URL has the required components
				parsedURL, err := url.Parse(signedURL)
				if err != nil {
					t.Errorf("Failed to parse signed URL: %v", err)
					return
				}
				fmt.Println(parsedURL.String())
				// Check that the signature parameters are present
				query := parsedURL.Query()
				if query.Get("x-signature") == "" {
					t.Error("Signed URL missing x-signature parameter")
				}
				if query.Get("x-expire") == "" && strings.HasPrefix(tt.path, "/files") {
					t.Error("Signed URL missing x-expire parameter")
				}

				// Verify the original path is preserved (without leading slash)
				expectedPath := strings.TrimPrefix(tt.path, "/")
				if !strings.HasSuffix(parsedURL.Path, expectedPath) {
					t.Errorf("Signed URL path does not match original. got=%s, want suffix=%s", parsedURL.Path, expectedPath)
				}
			}
		})
	}
}

func TestClient_Get(t *testing.T) {
	expectedContent := []byte("test content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}
		if r.URL.Path != "/files/test.jpg" {
			t.Errorf("expected path /files/test.jpg, got %s", r.URL.Path)
		}
		w.Write(expectedContent)
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	client := &Client{
		URL:       serverURL,
		transport: http.DefaultTransport,
	}

	res, err := client.Get("/test.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	content, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, expectedContent) {
		t.Errorf("expected %s, got %s", expectedContent, content)
	}
}

func TestClient_Put(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		content       []byte
		serverHandler func(w http.ResponseWriter, r *http.Request)
		wantErr       bool
		errorContains string
	}{
		{
			name:    "successful upload",
			key:     "test.jpg",
			content: []byte("test content"),
			wantErr: false,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read request body: %v", err)
				}
				if !bytes.Equal(body, []byte("test content")) {
					t.Errorf("expected body %q, got %q", "test content", string(body))
				}
				w.WriteHeader(http.StatusCreated)
			},
		},
		{
			name:          "server error with message",
			key:           "test.jpg",
			content:       []byte("test content"),
			wantErr:       true,
			errorContains: "unexpected status code 500: internal server error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("internal server error"))
			},
		},
		{
			name:          "server error no message",
			key:           "test.jpg",
			content:       []byte("test content"),
			wantErr:       true,
			errorContains: "unexpected status code 500",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name:    "empty content",
			key:     "empty.txt",
			content: []byte{},
			wantErr: false,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read request body: %v", err)
				}
				if len(body) != 0 {
					t.Errorf("expected empty body, got %q", string(body))
				}
				w.WriteHeader(http.StatusCreated)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Check HTTP method
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT request, got %s", r.Method)
				}

				// Handle the request using test case handler
				tt.serverHandler(w, r)
			}))
			defer server.Close()

			// Create client
			serverURL, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("failed to parse server URL: %v", err)
			}

			client := &Client{
				URL:       serverURL,
				transport: http.DefaultTransport,
			}

			// Execute Put method
			err = client.Put(tt.key, bytes.NewReader(tt.content))

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// Helper function to test ReadCloser is properly closed
type testReadCloser struct {
	*bytes.Reader
	closed bool
}

func newTestReadCloser(data []byte) *testReadCloser {
	return &testReadCloser{
		Reader: bytes.NewReader(data),
		closed: false,
	}
}

func (t *testReadCloser) Close() error {
	t.closed = true
	return nil
}

func TestClient_Put_ReaderClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse server URL: %v", err)
	}

	client := &Client{
		URL:       serverURL,
		transport: http.DefaultTransport,
	}

	reader := newTestReadCloser([]byte("test content"))
	err = client.Put("test.txt", reader)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !reader.closed {
		t.Error("reader was not closed")
	}
}

func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE request, got %s", r.Method)
		}
		if r.URL.Path != "/files/test.jpg" {
			t.Errorf("expected path /files/test.jpg, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	client := &Client{
		URL:       serverURL,
		transport: http.DefaultTransport,
	}

	err := client.Delete("/test.jpg")
	if err != nil {
		t.Fatal(err)
	}
}
func TestClient_List(t *testing.T) {
	expectedResult := &ListResult{
		Keys:     []string{"test1.jpg", "test2.jpg"},
		NextPage: "next",
		HasMore:  true,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}
		if r.URL.Path != "/files" {
			t.Errorf("expected path /files, got %s", r.URL.Path)
		}

		q := r.URL.Query()
		if limit := q.Get("limit"); limit != "10" {
			t.Errorf("expected limit=10, got %s", limit)
		}
		if start := q.Get("starting_at"); start != "start" {
			t.Errorf("expected start=start, got %s", start)
		}
		if unlinked := q.Get("unlinked"); unlinked != "true" {
			t.Errorf("expected unlinked=true, got %s", unlinked)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expectedResult)
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	client := &Client{
		URL:       serverURL,
		transport: http.DefaultTransport,
	}

	result, err := client.List(ListOptions{
		Limit:      10,
		StartingAt: "start",
		Unlinked:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result, expectedResult) {
		t.Errorf("expected %+v, got %+v", expectedResult, result)
	}
}
