package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"koha-rfid/internal/rfidops"
)

// loggingResponseWriter wraps http.ResponseWriter to capture the status code
// for request logging.
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (lw *loggingResponseWriter) WriteHeader(code int) {
	lw.status = code
	lw.ResponseWriter.WriteHeader(code)
}

// HttpServer provides the local HTTP API for the Koha JavaScript integration.
type HttpServer struct {
	listen  string
	rfidOps rfidops.RfidOps
	debug   bool
	tlsCert string // path to TLS cert (if empty, serve HTTP)
	tlsKey  string // path to TLS key
}

func NewHttpServer(listen string, ops rfidops.RfidOps, debug bool) *HttpServer {
	return &HttpServer{
		listen:   listen,
		rfidOps:  ops,
		debug:    debug,
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
	mux.HandleFunc("/scan/", s.handleScan)
	mux.HandleFunc("/secure", s.handleSecure)
	mux.HandleFunc("/program", s.handleProgram)

	// Mock control endpoints — only functional when rfidOps is *mockOps
	mux.HandleFunc("/mock/tag", s.handleMockTag)
	mux.HandleFunc("/mock/clear", s.handleMockClear)
	mux.HandleFunc("/mock/remove", s.handleMockRemove)
	mux.HandleFunc("/mock/error", s.handleMockError)
	mux.HandleFunc("/mock/timeout", s.handleMockTimeout)
	mux.HandleFunc("/mock/set", s.handleMockSet)
	mux.HandleFunc("/mock/status", s.handleMockStatus)
	mux.HandleFunc("/mock/reset", s.handleMockReset)

	addr := s.listen
	if addr == "" {
		addr = "localhost:9000"
	}

	// Wrap mux with CORS headers and request logging for every request
	var handler http.Handler = mux
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		corsHeader(w)
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			if s.debug {
				log.Printf("HTTP %s %s -> 204 in %v", r.Method, r.URL.Path, time.Since(start))
			}
			return
		}
		// Wrap the response writer to capture status code
		lw := &loggingResponseWriter{ResponseWriter: w, status: 200}
		mux.ServeHTTP(lw, r)
		if s.debug {
			log.Printf("HTTP %s %s -> %d in %v", r.Method, r.URL.Path, lw.status, time.Since(start))
		}
	})
	log.Printf("HTTPS server listening on %s", addr)
	return http.ListenAndServeTLS(addr, s.tlsCert, s.tlsKey, handler)
}

func (s *HttpServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html><html><head><title>RFID Server</title></head><body>
<h1>RFID Server</h1><p>Status: OK</p></body></html>`
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(200)
	w.Write([]byte(html))
}

// ---------------------------------------------------------------------------
// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// ---------------------------------------------------------------------------
// Scan handler

func (s *HttpServer) handleScan(w http.ResponseWriter, r *http.Request) {

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
		http.Error(w, fmt.Sprintf("RFID error: %v", err), 504)
		return
	}

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
	writeJSON(w, jsonBody)
}

// ---------------------------------------------------------------------------
// Secure handlers

func (s *HttpServer) handleSecure(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	var ops []rfidops.SecureOp
	for key, vals := range r.Form {
		if len(key) != 16 || strings.HasPrefix(key, "call") || strings.HasPrefix(key, "_") {
			continue
		}
		ops = append(ops, rfidops.SecureOp{SID: key, AfiHex: vals[0]})
	}

	res := rfidops.Secure(s.rfidOps, ops)
	if res.Error != "" {
		jsonBody, _ := json.Marshal(res)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(jsonBody)
		return
	}
	jsonBody, _ := json.Marshal(res)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBody)
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
	jsonBody, _ := json.Marshal(res)
	writeJSON(w, jsonBody)
}


