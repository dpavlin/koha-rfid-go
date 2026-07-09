package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"

	"koha-rfid/internal/rfid"
	"koha-rfid/internal/rfidops"
)

// ---------------------------------------------------------------------------
// Stateful mock RfidOps — controllable via HTTP endpoints for browser testing.

type mockTag struct {
	SID      string `json:"sid"`
	Content  string `json:"content"`
	Security string `json:"security"` // "DA" or "D7"
}

type mockState struct {
	mu           sync.Mutex
	tags         []mockTag
	errorCount   int // remaining /scan/ calls that return error
	timeoutCount int // remaining /scan/ calls that hang until timeout
}

// mockOps implements rfidops.RfidOps using an in-memory tag list.
type mockOps struct {
	state *mockState
}

func newMockOps() *mockOps {
	return &mockOps{state: &mockState{}}
}

func (m *mockOps) Inventory() ([]string, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()

	// Error mode — return a generic error so the JavaScript shows "RFID scan error"
	if m.state.errorCount > 0 {
		m.state.errorCount--
		return nil, errors.New("RFID reader error")
	}
	// Timeout mode — return timeout error to trigger timeout handling
	if m.state.timeoutCount > 0 {
		m.state.timeoutCount--
		return nil, rfidops.ErrReaderTimeout
	}

	sids := make([]string, len(m.state.tags))
	for i, t := range m.state.tags {
		sids[i] = t.SID
	}
	return sids, nil
}

func (m *mockOps) InventoryWithReset() ([]string, error) {
	return m.Inventory()
}

func (m *mockOps) ReadAfi(tag string) (byte, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	for _, t := range m.state.tags {
		if strings.EqualFold(t.SID, tag) {
			afiHex := t.Security
			if afiHex == "" {
				afiHex = "DA"
			}
			b, err := hex.DecodeString(afiHex)
			if err != nil || len(b) != 1 {
				return rfid.AfiUnsecure, nil
			}
			return b[0], nil
		}
	}
	return rfid.AfiUnsecure, nil
}

func (m *mockOps) ReadBlocks(tag string, start, count int) (map[int]string, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	for _, t := range m.state.tags {
		if strings.EqualFold(t.SID, tag) {
			// Encode content as RFID501 blocks
			content := t.Content
			if content == "" {
				return map[int]string{}, nil
			}
			tag501 := rfid.RFID501Tag{Set: 1, Total: 1, Type: 0, Content: content, Branch: 0, Library: 0, Custom: 0}
			encodedBlocks := rfid.EncodeRFID501(&tag501)
			blocks := make(map[int]string)
			for i, b := range encodedBlocks {
				if i >= start && i < start+count {
					blocks[i] = b
				}
			}
			return blocks, nil
		}
	}
	return map[int]string{}, nil
}

func (m *mockOps) WriteBlocks(tag string, data string) error {
	// For mock mode, we just log the write
	log.Printf("mock: WriteBlocks(%s, %s)", tag, data)
	return nil
}

func (m *mockOps) WriteAfi(tag string, afi byte) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	afiHex := hex.EncodeToString([]byte{afi})
	for i := range m.state.tags {
		if strings.EqualFold(m.state.tags[i].SID, tag) {
			m.state.tags[i].Security = afiHex
		}
	}
	log.Printf("mock: WriteAfi(%s, %s)", tag, afiHex)
	return nil
}

func (m *mockOps) Lock()   {}
func (m *mockOps) Unlock() {}

// ---------------------------------------------------------------------------
// Mock control endpoints (registered in server.go when mock mode is enabled)

func (s *HttpServer) handleMockTag(w http.ResponseWriter, r *http.Request) {
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

	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	ops.state.tags = append(ops.state.tags, t)
	ops.state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func (s *HttpServer) handleMockClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	ops.state.tags = nil
	ops.state.errorCount = 0
	ops.state.timeoutCount = 0
	ops.state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func (s *HttpServer) handleMockRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var opts struct{ SID string }
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if len(opts.SID) != 16 {
		http.Error(w, "sid must be 16 hex chars", 400)
		return
	}

	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	found := false
	for i := range ops.state.tags {
		if strings.EqualFold(ops.state.tags[i].SID, opts.SID) {
			ops.state.tags = append(ops.state.tags[:i], ops.state.tags[i+1:]...)
			found = true
			break
		}
	}
	ops.state.mu.Unlock()

	if found {
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":1}`))
	} else {
		http.Error(w, `{"ok":0,"error":"tag not found"}`, 404)
	}
}

func (s *HttpServer) handleMockError(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var opts struct{ Count int }
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	ops.state.errorCount = opts.Count
	ops.state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func (s *HttpServer) handleMockTimeout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var opts struct{ Count int }
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	ops.state.timeoutCount = opts.Count
	ops.state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func (s *HttpServer) handleMockSet(w http.ResponseWriter, r *http.Request) {
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
	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	ops.state.tags = tags
	ops.state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}

func (s *HttpServer) handleMockStatus(w http.ResponseWriter, r *http.Request) {
	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	tags := make([]mockTag, len(ops.state.tags))
	copy(tags, ops.state.tags)
	ec := ops.state.errorCount
	tc := ops.state.timeoutCount
	ops.state.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{
		"tags":         tags,
		"errorCount":   ec,
		"timeoutCount": tc,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(body)
}

func (s *HttpServer) handleMockReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ops, ok := s.rfidOps.(*mockOps)
	if !ok {
		http.Error(w, "not in mock mode", 500)
		return
	}
	ops.state.mu.Lock()
	ops.state.tags = nil
	ops.state.errorCount = 0
	ops.state.timeoutCount = 0
	ops.state.mu.Unlock()

	w.WriteHeader(200)
	w.Write([]byte(`{"ok":1}`))
}
