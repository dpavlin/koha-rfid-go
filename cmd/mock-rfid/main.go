// Mock RFID server — controllable via HTTP to simulate RFID events for JS testing.
//
// Run:
//   go run ./cmd/mock-rfid [-port 9000] [-tls]
//
// Real endpoints (same as koha-rfid-go):
//   GET  /ping          → {"status":"ok"}
//   GET  /scan/         → {"tags":[{"sid":"...","content":"...","security":"...",...}]}
//   GET  /secure.js?<sid>=DA&callback=jsonp → write AFI, returns JSONP
//
// Control endpoints (drive the simulation):
//   POST /mock/tag      — add a tag  (JSON: {"sid":"16hex","content":"130...","security":"DA"})
//   POST /mock/clear    — remove all tags
//   POST /mock/error    — set error mode (JSON: {"count":N}) — next N /scan/ calls return 500
//   POST /mock/timeout  — set timeout mode (JSON: {"count":N}) — next N /scan/ calls hang
//   POST /mock/set      — replace all tags (JSON array of tag objects)
//   GET  /mock/status   — show current inventory
//   POST /mock/reset    — reset all state (tags, error/timeout counters)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Mock state

type mockTag struct {
	SID      string `json:"sid"`
	Content  string `json:"content"`
	Security string `json:"security"` // "DA" or "D7"
}

type mockState struct {
	mu            sync.Mutex
	tags          []mockTag
	errorCount    int // remaining /scan/ calls that return error
	timeoutCount  int // remaining /scan/ calls that hang until timeout
}

var state = &mockState{}

// ---------------------------------------------------------------------------
// Real endpoints (same as koha-rfid-go)

func handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleScan(w http.ResponseWriter, r *http.Request) {
	callback := r.FormValue("callback")

	state.mu.Lock()
	ec := state.errorCount
	tc := state.timeoutCount
	state.mu.Unlock()

	// Timeout mode — hang until the browser's fetch timeout fires
	if tc > 0 {
		state.mu.Lock()
		state.timeoutCount--
		state.mu.Unlock()
		time.Sleep(30 * time.Second) // longer than browser's 15s fetch timeout
		w.WriteHeader(504)
		w.Write([]byte(`{"error":"timeout"}`))
		return
	}

	// Error mode — return 500
	if ec > 0 {
		state.mu.Lock()
		state.errorCount--
		state.mu.Unlock()
		http.Error(w, `{"error":"mock reader error"}`, 500)
		return
	}

	state.mu.Lock()
	tags := make([]mockTag, len(state.tags))
	copy(tags, state.tags)
	state.mu.Unlock()

	type tagItem struct {
		SID      string `json:"sid"`
		Content  string `json:"content"`
		Security string `json:"security"`
		TagType  string `json:"tag_type"`
		Reader   string `json:"reader"`
	}
	items := make([]tagItem, len(tags))
	for i, t := range tags {
		items[i] = tagItem{
			SID:      t.SID,
			Content:  t.Content,
			Security: t.Security,
			TagType:  "RFID501",
			Reader:   "mock",
		}
	}

	body, _ := json.Marshal(map[string]interface{}{"tags": items})
	writeJSONP(w, callback, body)
}

func handleSecureJSONP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	callback := r.FormValue("callback")

	state.mu.Lock()
	// Find the tag and update its security
	for key, vals := range r.Form {
		if len(key) != 16 || strings.HasPrefix(key, "call") || strings.HasPrefix(key, "_") {
			continue
		}
		target := strings.ToUpper(vals[0])
		for i := range state.tags {
			if strings.EqualFold(state.tags[i].SID, key) {
				state.tags[i].Security = target
			}
		}
	}
	state.mu.Unlock()

	resp := map[string]interface{}{"ok": 1, "error": ""}
	jsonBody, _ := json.Marshal(resp)
	writeJSONP(w, callback, jsonBody)
}

// ---------------------------------------------------------------------------
// Control endpoints

func handleMockTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var t mockTag
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if len(t.SID) != 16 {
		http.Error(w, "sid must be 16 hex chars", 400)
		return
	}
	t.Security = strings.ToUpper(t.Security)

	state.mu.Lock()
	state.tags = append(state.tags, t)
	state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func handleMockClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	state.mu.Lock()
	state.tags = nil
	state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func handleMockError(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var opts struct{ Count int }
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	state.mu.Lock()
	state.errorCount = opts.Count
	state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func handleMockTimeout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var opts struct{ Count int }
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	state.mu.Lock()
	state.timeoutCount = opts.Count
	state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func handleMockSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var tags []mockTag
	if err := json.NewDecoder(r.Body).Decode(&tags); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	for i := range tags {
		tags[i].Security = strings.ToUpper(tags[i].Security)
	}
	state.mu.Lock()
	state.tags = tags
	state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func handleMockStatus(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	tags := make([]mockTag, len(state.tags))
	copy(tags, state.tags)
	ec := state.errorCount
	tc := state.timeoutCount
	state.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{
		"tags":         tags,
		"errorCount":   ec,
		"timeoutCount": tc,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(body)
}

func handleMockReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	state.mu.Lock()
	state.tags = nil
	state.errorCount = 0
	state.timeoutCount = 0
	state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

// ---------------------------------------------------------------------------
// Helpers

func writeJSONP(w http.ResponseWriter, callback string, body []byte) {
	if callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "%s(%s)", callback, body)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

func corsHeader(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}

// ---------------------------------------------------------------------------
// Main

func main() {
	port := flag.String("port", "9000", "listen port")
	tls := flag.Bool("tls", false, "enable HTTPS with self-signed cert")
	flag.Parse()

	addr := "localhost:" + *port

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/scan/", handleScan)
	mux.HandleFunc("/secure.js", handleSecureJSONP)
	mux.HandleFunc("/mock/tag", handleMockTag)
	mux.HandleFunc("/mock/clear", handleMockClear)
	mux.HandleFunc("/mock/error", handleMockError)
	mux.HandleFunc("/mock/timeout", handleMockTimeout)
	mux.HandleFunc("/mock/set", handleMockSet)
	mux.HandleFunc("/mock/status", handleMockStatus)
	mux.HandleFunc("/mock/reset", handleMockReset)

	var handler http.Handler = mux
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
		mux.ServeHTTP(w, r)
	})

	if *tls {
		log.Printf("Starting mock RFID HTTPS server on %s", addr)
		err := http.ListenAndServeTLS(addr, "rfid-localhost.crt", "rfid-localhost.key", handler)
		log.Fatalf("HTTPS server error: %v", err)
	} else {
		log.Printf("Starting mock RFID HTTP server on %s", addr)
		err := http.ListenAndServe(addr, handler)
		log.Fatalf("HTTP server error: %v", err)
	}
}
