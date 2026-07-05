package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"koha-rfid/internal/rfidops"
)

// HttpServer provides the local JSONP API for the Koha JavaScript integration.
type HttpServer struct {
	listen   string
	mu       sync.Mutex
	rfidOps  rfidops.RfidOps
	debug    bool
	tagCache map[string]*rfidops.TagInfo
	tlsCert  string // path to TLS cert (if empty, serve HTTP)
	tlsKey   string // path to TLS key
}

func NewHttpServer(listen string, ops rfidops.RfidOps, debug bool) *HttpServer {
	return &HttpServer{
		listen:   listen,
		rfidOps:  ops,
		debug:    debug,
		tagCache: make(map[string]*rfidops.TagInfo),
	}
}

// SetTLS enables HTTPS with the given cert/key files.
func (s *HttpServer) SetTLS(cert, key string) {
	s.tlsCert = cert
	s.tlsKey = key
}

// corsHeader adds permissive CORS headers so browsers can fetch from any origin
// (e.g., the Koha HTTPS page fetching from localhost).
func corsHeader(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}

func (s *HttpServer) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/ping", s.handlePing)
	mux.HandleFunc("/scan/", s.handleScan)
	mux.HandleFunc("/scan/only/", s.handleScan)
	mux.HandleFunc("/secure", s.handleSecure)
	mux.HandleFunc("/secure.js", s.handleSecureJSONP)
	mux.HandleFunc("/program", s.handleProgram)

	// Static file serving for the JavaScript example
	mux.Handle("/examples/", http.StripPrefix("/examples/", http.FileServer(http.Dir("examples"))))

	addr := s.listen
	if addr == "" {
		addr = "localhost:9000"
	}

	// Wrap mux with CORS headers for every request
	var handler http.Handler = mux
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		mux.ServeHTTP(w, r)
	})

	if s.tlsCert != "" && s.tlsKey != "" {
		log.Printf("HTTPS server listening on %s", addr)
		return http.ListenAndServeTLS(addr, s.tlsCert, s.tlsKey, handler)
	}

	log.Printf("HTTP server listening on %s", addr)
	return http.ListenAndServe(addr, handler)
}

func (s *HttpServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html><html><head><title>RFID Server</title></head><body>
<h1>RFID Server</h1><p>Status: OK</p></body></html>`
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(200)
	w.Write([]byte(html))
}

func (s *HttpServer) handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(`{"status":"ok"}`))
}

// ---------------------------------------------------------------------------
// writeJSONP writes a JSON body wrapped in a callback if present.
func writeJSONP(w http.ResponseWriter, callback string, body []byte) {
	if callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "%s(%s)", callback, body)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

// ---------------------------------------------------------------------------
// Scan handler

func (s *HttpServer) handleScan(w http.ResponseWriter, r *http.Request) {
	callback := r.FormValue("callback")

	// Retry scan on CRC/communication errors
	var result *rfidops.ScanResult
	var err error
	for i := 0; i < 3; i++ {
		result, err = rfidops.Scan(s.rfidOps)
		if err == nil {
			break
		}
		if s.debug {
			log.Printf("scan retry %d/3: %v", i+1, err)
		}
		// Brief pause before retry to let reader recover
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("RFID error: %v", err), 500)
		return
	}

	// Update cache
	s.mu.Lock()
	for _, info := range result.Tags {
		infoCopy := info
		s.tagCache[info.SID] = &infoCopy
	}
	s.mu.Unlock()

	// Build JSON response
	type tagItem struct {
		SID      string `json:"sid"`
		Content  string `json:"content"`
		Security string `json:"security"`
		TagType  string `json:"tag_type"`
		Reader   string `json:"reader"`
	}
	items := make([]tagItem, len(result.Tags))
	for i, info := range result.Tags {
		items[i] = tagItem{
			SID:      info.SID,
			Content:  info.Content,
			Security: info.Security,
			TagType:  info.TagType,
			Reader:   info.Reader,
		}
	}

	jsonBody, _ := json.Marshal(map[string]interface{}{
		"tags": items,
	})
	writeJSONP(w, callback, jsonBody)
}

// ---------------------------------------------------------------------------
// Secure handlers

func (s *HttpServer) handleSecure(w http.ResponseWriter, r *http.Request) {
	status := 302
	r.ParseForm()

	var ops []rfidops.SecureOp
	for key, vals := range r.Form {
		if len(key) == 16 {
			ops = append(ops, rfidops.SecureOp{SID: key, AfiHex: vals[0]})
			status = 200
		}
	}

	res := rfidops.Secure(s.rfidOps, ops)
	if res.Error != "" {
		http.Error(w, res.Error, 400)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("http://%s/", s.listen))
	w.WriteHeader(status)
}

func (s *HttpServer) handleSecureJSONP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	callback := r.FormValue("callback")

	var ops []rfidops.SecureOp
	for key, vals := range r.Form {
		if len(key) != 16 || strings.HasPrefix(key, "call") || strings.HasPrefix(key, "_") {
			continue
		}
		ops = append(ops, rfidops.SecureOp{SID: key, AfiHex: vals[0]})
	}

	res := rfidops.Secure(s.rfidOps, ops)
	jsonBody, _ := json.Marshal(res)
	writeJSONP(w, callback, jsonBody)
}

// ---------------------------------------------------------------------------
// Program handler

func (s *HttpServer) handleProgram(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form parse error", 400)
		return
	}

	var ops []rfidops.ProgramOp
	for key, vals := range r.Form {
		if len(key) != 16 || strings.HasPrefix(key, "call") || strings.HasPrefix(key, "_") {
			continue
		}
		ops = append(ops, rfidops.ProgramOp{SID: key, Content: vals[0]})
	}

	res := rfidops.Program(s.rfidOps, ops)
	if len(res.Errors) > 0 {
		errResp, _ := json.Marshal(res)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write(errResp)
		return
	}

	callback := r.FormValue("callback")
	jsonBody, _ := json.Marshal(res)
	writeJSONP(w, callback, jsonBody)
}

// ---------------------------------------------------------------------------
// Background scan – called from main.go, not a handler
func (s *HttpServer) BackgroundScan() error {
	result, err := rfidops.Scan(s.rfidOps)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, info := range result.Tags {
		infoCopy := info
		s.tagCache[info.SID] = &infoCopy
	}

	// Remove stale tags
	seen := make(map[string]bool, len(result.Tags))
	for _, info := range result.Tags {
		seen[info.SID] = true
	}
	for sid := range s.tagCache {
		if !seen[sid] {
			delete(s.tagCache, sid)
		}
	}
	return nil
}
