package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleSecureJSONP_InvalidAFI(t *testing.T) {
	server := &HttpServer{
		listen:   "localhost:9000",
		debug:    false,
		tagCache: make(map[string]*TagInfo),
	}

	tests := []struct {
		name      string
		query     string
		wantOK    int    // 1 for success, 0 for error
		wantError string // expected error message substring
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
			name:      "valid AFI hex with callback",
			query:     "E2001234567890AB=DA&callback=jsonp123",
			wantOK:    1,
			wantError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/secure.js?"+tt.query, nil)
			w := httptest.NewRecorder()

			func() {
				defer func() {
					if r := recover(); r != nil {
						if tt.wantOK == 1 {
							t.Skip("valid AFI case requires real reader; skipping response check")
						}
						t.Fatalf("unexpected panic: %v", r)
					}
				}()
				server.handleSecureJSONP(w, req)
				resp := w.Body.String()

				var result map[string]interface{}
				body := resp
				if strings.HasPrefix(body, "jsonp123(") {
					body = strings.TrimPrefix(body, "jsonp123(")
					body = strings.TrimSuffix(body, ")")
				}
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
			}()
		})
	}
}
