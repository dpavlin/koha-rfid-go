package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"koha-rfid/internal/rfidops"
)

// ---------------------------------------------------------------------------
// Stub RfidOps for tests

type stubOps struct {
	rfidops.RfidOps                   // embed interface so we only override what we need
	inventoryFn    func() ([]string, error)
	readAfiFn      func(tag string) (byte, error)
	readBlocksFn   func(tag string, start, count int) (map[int]string, error)
	writeBlocksFn  func(tag string, data string) error
	writeAfiFn     func(tag string, afi byte) error
}

func (m stubOps) Inventory() ([]string, error) {
	if m.inventoryFn != nil {
		return m.inventoryFn()
	}
	return nil, nil
}

func (m stubOps) ReadAfi(tag string) (byte, error) {
	if m.readAfiFn != nil {
		return m.readAfiFn(tag)
	}
	return 0, nil
}

func (m stubOps) ReadBlocks(tag string, start, count int) (map[int]string, error) {
	if m.readBlocksFn != nil {
		return m.readBlocksFn(tag, start, count)
	}
	return nil, nil
}

func (m stubOps) WriteBlocks(tag string, data string) error {
	if m.writeBlocksFn != nil {
		return m.writeBlocksFn(tag, data)
	}
	return nil
}

func (m stubOps) WriteAfi(tag string, afi byte) error {
	if m.writeAfiFn != nil {
		return m.writeAfiFn(tag, afi)
	}
	return nil
}

func (m stubOps) InventoryWithReset() ([]string, error) {
	if m.inventoryFn != nil {
		return m.inventoryFn()
	}
	return nil, nil
}

func (m stubOps) Lock()    {}
func (m stubOps) Unlock()  {}

// ---------------------------------------------------------------------------
// Helpers

// newTestServer creates an HttpServer with stub ops and optional listen address.
func newTestServer(listen string) *HttpServer {
	return NewHttpServer(listen, stubOps{}, false)
}

// newTestServerWithOps creates an HttpServer with a custom stubOps.
func newTestServerWithOps(m stubOps) *HttpServer {
	return NewHttpServer("", m, false)
}

// ---------------------------------------------------------------------------
// Tests

func TestHandleIndex(t *testing.T) {
	server := newTestServer("")
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// TestHandleScan tests the scan handler with mock data.
func TestHandleScan(t *testing.T) {
	m := stubOps{
		inventoryFn: func() ([]string, error) {
			return []string{"E2001234567890AB"}, nil
		},
		readAfiFn: func(tag string) (byte, error) {
			return 0xDA, nil
		},
		readBlocksFn: func(tag string, start, count int) (map[int]string, error) {
			// Return a valid RFID501 block (content "1301234567")
			return map[int]string{
				0: "3133303132333435", // "13012345" ASCII
				1: "3637000000000000", // "67" + padding
			}, nil
		},
	}
	server := newTestServerWithOps(m)
	req := httptest.NewRequest("GET", "/scan/", nil)
	w := httptest.NewRecorder()
	server.handleScan(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json parse error: %v", err)
	}
	tags, ok := resp["tags"].([]interface{})
	if !ok {
		t.Fatalf("missing 'tags' array")
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
}

func TestHandleScanError(t *testing.T) {
	m := stubOps{
		inventoryFn: func() ([]string, error) {
			return nil, fmt.Errorf("mock inventory error")
		},
	}
	server := newTestServerWithOps(m)
	req := httptest.NewRequest("GET", "/scan/", nil)
	w := httptest.NewRecorder()
	server.handleScan(w, req)

	if w.Code != 504 {
		t.Errorf("status = %d, want 504 (Gateway Timeout)", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Secure handler tests

func TestHandleSecureSuccess(t *testing.T) {
	m := stubOps{
		writeAfiFn: func(tag string, afi byte) error {
			return nil
		},
	}
	server := newTestServerWithOps(m)
	req := httptest.NewRequest("GET", "/secure?E2001234567890AB=DA", nil)
	w := httptest.NewRecorder()
	server.handleSecure(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleSecureError(t *testing.T) {
	m := stubOps{
		writeAfiFn: func(tag string, afi byte) error {
			return fmt.Errorf("mock write error")
		},
	}
	server := newTestServerWithOps(m)
	req := httptest.NewRequest("GET", "/secure?E2001234567890AB=DA", nil)
	w := httptest.NewRecorder()
	server.handleSecure(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Secure handler non-reader tests (parameter validation)

func TestHandleSecureNonReader(t *testing.T) {
	m := stubOps{
		writeAfiFn: func(tag string, afi byte) error {
			return nil
		},
	}
	server := newTestServerWithOps(m)

	tests := []struct {
		name      string
		query     string
		wantOK    int
		wantError string
	}{
		{
			name:      "invalid AFI hex – bad chars",
			query:     "E2001234567890AB=ZZ",
			wantOK:    0,
			wantError: "invalid AFI hex",
		},
		{
			name:      "invalid AFI hex – too long",
			query:     "E2001234567890AB=DAFF",
			wantOK:    0,
			wantError: "invalid AFI hex",
		},
		{
			name:      "invalid AFI hex with extra params",
			query:     "E2001234567890AB=ZZ&callback=jsonp123",
			wantOK:    0,
			wantError: "invalid AFI hex",
		},
		{
			name:      "short key (<16 chars) – skipped, no tags processed",
			query:     "E20012345678AB=DA",
			wantOK:    1,
			wantError: "",
		},
		{
			name:      "key starting with 'call' – skipped",
			query:     "callback1234567890=DA",
			wantOK:    1,
			wantError: "",
		},
		{
			name:      "key starting with '_' – skipped",
			query:     "_E2001234567890AB=DA",
			wantOK:    1,
			wantError: "",
		},
		{
			name:      "no form keys at all",
			query:     "",
			wantOK:    1,
			wantError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/secure?"+tt.query, nil)
			w := httptest.NewRecorder()
			server.handleSecure(w, req)

			resp := w.Body.String()
			ct := w.Header().Get("Content-Type")

			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var result map[string]interface{}
			body := resp
			if err := json.Unmarshal([]byte(body), &result); err != nil {
				t.Fatalf("json parse error: %v\nraw body: %s", err, resp)
			}

			okVal, ok := result["ok"].(float64)
			if !ok {
				t.Fatalf("missing 'ok' field in response: %v", result)
			}
			if int(okVal) != tt.wantOK {
				t.Errorf("ok = %d, want %d", int(okVal), tt.wantOK)
			}

			if tt.wantError != "" {
				errStr, hasErr := result["error"].(string)
				if !hasErr {
					t.Errorf("missing 'error' field in response")
				} else if !strings.Contains(errStr, tt.wantError) {
					t.Errorf("error = %q, want substring %q", errStr, tt.wantError)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Program handler tests

func TestHandleProgramNonReader(t *testing.T) {
	m := stubOps{
		writeBlocksFn: func(tag string, data string) error {
			return nil
		},
		writeAfiFn: func(tag string, afi byte) error {
			return nil
		},
	}
	server := newTestServerWithOps(m)

	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "short key (<16 chars) – skipped",
			query: "E20012345678AB=1301234567",
		},
		{
			name:  "key starting with 'call' – skipped",
			query: "callback1234567890=1301234567",
		},
		{
			name:  "key starting with '_' – skipped",
			query: "_E2001234567890AB=1301234567",
		},
		{
			name:  "no form keys at all",
			query: "",
		},
		{
			name:  "callback param ignored (no tag keys)",
			query: "callback=jsonp123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/program?"+tt.query, nil)
			w := httptest.NewRecorder()
			server.handleProgram(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}

			resp := w.Body.String()
			ct := w.Header().Get("Content-Type")

			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var result map[string]interface{}
			body := resp
			if err := json.Unmarshal([]byte(body), &result); err != nil {
				t.Fatalf("json parse error: %v\nraw body: %s", err, resp)
			}

			okVal, ok := result["ok"].(float64)
			if !ok {
				t.Fatalf("missing 'ok' field in response: %v", result)
			}
			if int(okVal) != 1 {
				t.Errorf("ok = %d, want 1", int(okVal))
			}
		})
	}
}

func TestHandleProgramSuccess(t *testing.T) {
	m := stubOps{
		writeBlocksFn: func(tag string, data string) error {
			return nil
		},
		writeAfiFn: func(tag string, afi byte) error {
			return nil
		},
	}
	server := newTestServerWithOps(m)
	req := httptest.NewRequest("GET", "/program?E2001234567890AB=1301234567", nil)
	w := httptest.NewRecorder()
	server.handleProgram(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("json parse error: %v", err)
	}
	okVal, ok := result["ok"].(float64)
	if !ok {
		t.Fatalf("missing 'ok'")
	}
	if int(okVal) != 1 {
		t.Errorf("ok = %d, want 1", int(okVal))
	}
}

func TestHandleProgramError(t *testing.T) {
	m := stubOps{
		writeBlocksFn: func(tag string, data string) error {
			return fmt.Errorf("mock write error")
		},
	}
	server := newTestServerWithOps(m)
	req := httptest.NewRequest("GET", "/program?E2001234567890AB=1301234567", nil)
	w := httptest.NewRecorder()
	server.handleProgram(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// NewHttpServer tests

func TestNewHttpServer(t *testing.T) {
	server := NewHttpServer("", stubOps{}, true)

	if server.listen != "" {
		t.Errorf("listen = %q, want empty (Run() applies default)", server.listen)
	}
	if server.debug != true {
		t.Errorf("debug = %v, want true", server.debug)
	}
	if server.rfidOps == nil {
		t.Errorf("rfidOps should be non-nil")
	}
}

func TestRunMuxRegistration(t *testing.T) {
	server := NewHttpServer("", stubOps{}, false)
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleIndex)
	mux.HandleFunc("/scan/", server.handleScan)
	mux.HandleFunc("/secure", server.handleSecure)
	mux.HandleFunc("/program", server.handleProgram)

	// Verify each handler responds (we don't start the server, just check routing)
	tests := []struct {
		path     string
		wantCode int
	}{
		{"/", 200},
		{"/scan/", 200},
		{"/secure", 200},
		{"/program", 200},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != tt.wantCode {
			t.Errorf("%s status = %d, want %d", tt.path, w.Code, tt.wantCode)
		}
	}
}
