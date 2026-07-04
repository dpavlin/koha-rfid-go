package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"koha-rfid/internal/rfid"
	"koha-rfid/internal/rfidops"
)

// ---------------------------------------------------------------------------
// Integration tests that require a real 3M 810 RFID reader on a serial port.
//
// These tests are skipped unless the environment variable RFID_PORT is set to
// a valid serial port (e.g., /dev/ttyUSB0 on Linux, COM3 on Windows).
//
// Run with:
//   RFID_PORT=/dev/ttyUSB0 go test -v -run 'Integration' -timeout 60s
// ---------------------------------------------------------------------------

func rfidPort() string {
	return os.Getenv("RFID_PORT")
}

func skipIfNoHardware(t *testing.T) {
	port := rfidPort()
	if port == "" {
		t.Skip("Skipping integration test: set RFID_PORT env var to a serial port with a 3M 810 reader")
	}
}

// openReader opens the RFID reader for integration tests.
func openReader(t *testing.T) *rfid.RfidReader {
	port := rfidPort()
	reader, err := rfid.NewRfidReader(port, false)
	if err != nil {
		t.Fatalf("open RFID reader on %s: %v", port, err)
	}
	return reader
}

// ---------------------------------------------------------------------------
// rfidops integration tests

func TestIntegrationScan(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	// Probe first to verify communication
	hwVer, err := reader.Probe()
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	t.Logf("Hardware version: %s", hwVer)

	// Run a real scan
	result, err := rfidops.Scan(reader)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	t.Logf("Tags found: %d", len(result.Tags))
	for _, info := range result.Tags {
		t.Logf("  SID=%s Content=%q Security=%s TagType=%s Reader=%s",
			info.SID, info.Content, info.Security, info.TagType, info.Reader)
	}
}

func TestIntegrationProgram(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	// Probe
	_, err := reader.Probe()
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	// Inventory to find a tag to program
	tags, err := reader.Inventory()
	if err != nil {
		t.Fatalf("Inventory failed: %v", err)
	}
	if len(tags) == 0 {
		t.Skip("No tags in reader field — place a tag near the antenna")
	}

	sid := tags[0]
	t.Logf("Programming tag %s with content 'TestIntegration'", sid)

	op := rfidops.ProgramOp{
		SID:     sid,
		Content: "TestIntegration",
	}
	res := rfidops.Program(reader, []rfidops.ProgramOp{op})
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			t.Errorf("Program error: %s", e)
		}
	}
	if res.OK != 1 {
		t.Fatal("Program did not return OK=1")
	}

	// Verify by scanning
	result, err := rfidops.Scan(reader)
	if err != nil {
		t.Fatalf("Verify scan failed: %v", err)
	}
	found := false
	for _, info := range result.Tags {
		if strings.EqualFold(info.SID, sid) {
			found = true
			t.Logf("Verified: SID=%s Content=%q", info.SID, info.Content)
			break
		}
	}
	if !found {
		t.Error("Tag not found in verify scan")
	}
}

func TestIntegrationSecure(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	// Probe
	_, err := reader.Probe()
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	// Inventory
	tags, err := reader.Inventory()
	if err != nil {
		t.Fatalf("Inventory failed: %v", err)
	}
	if len(tags) == 0 {
		t.Skip("No tags in reader field")
	}

	sid := tags[0]
	t.Logf("Setting AFI for %s to DA (secure)", sid)

	op := rfidops.SecureOp{
		SID:    sid,
		AfiHex: "DA",
	}
	res := rfidops.Secure(reader, []rfidops.SecureOp{op})
	if res.Error != "" {
		t.Fatalf("Secure error: %s", res.Error)
	}
	if res.OK != 1 {
		t.Fatal("Secure did not return OK=1")
	}

	// Verify AFI
	afi, err := reader.ReadAfi(sid)
	if err != nil {
		t.Fatalf("ReadAfi failed: %v", err)
	}
	if afi != 0xDA {
		t.Errorf("AFI = %02x, want DA", afi)
	}
	t.Logf("AFI verified: %02x", afi)
}

func TestIntegrationBlank(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	// Probe
	_, err := reader.Probe()
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	// Inventory
	tags, err := reader.Inventory()
	if err != nil {
		t.Fatalf("Inventory failed: %v", err)
	}
	if len(tags) == 0 {
		t.Skip("No tags in reader field")
	}

	sid := tags[0]
	t.Logf("Blanking tag %s", sid)

	op := rfidops.ProgramOp{
		SID:     sid,
		Content: "blank",
	}
	res := rfidops.Program(reader, []rfidops.ProgramOp{op})
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			t.Errorf("Blank error: %s", e)
		}
	}
	if res.OK != 1 {
		t.Fatal("Blank did not return OK=1")
	}
	t.Log("Blank write succeeded")
}

// ---------------------------------------------------------------------------
// HTTP handler integration tests (use real reader through the server)

func TestIntegrationHandleScan(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	server := NewHttpServer("", reader, false)

	req := httptest.NewRequest("GET", "/scan/", nil)
	w := httptest.NewRecorder()
	server.handleScan(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json parse error: %v", err)
	}
	tags, ok := resp["tags"].([]interface{})
	if !ok {
		t.Fatalf("missing 'tags' array")
	}
	t.Logf("HTTP scan found %d tags", len(tags))
	for i, tag := range tags {
		m, ok := tag.(map[string]interface{})
		if !ok {
			continue
		}
		t.Logf("  tag[%d]: sid=%s content=%s security=%s",
			i, m["sid"], m["content"], m["security"])
	}
}

func TestIntegrationHandleScanWithCallback(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	server := NewHttpServer("", reader, false)

	req := httptest.NewRequest("GET", "/scan/?callback=jsonp123", nil)
	w := httptest.NewRecorder()
	server.handleScan(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/javascript" {
		t.Errorf("Content-Type = %q, want application/javascript", ct)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "jsonp123(") {
		t.Errorf("response doesn't start with callback wrapper")
	}
	// Verify JSONP body parses as JSON
	inner := strings.TrimPrefix(body, "jsonp123(")
	inner = strings.TrimSuffix(inner, ")")
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(inner), &resp); err != nil {
		t.Fatalf("JSONP inner parse error: %v", err)
	}
}

func TestIntegrationHandleSecure(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	// Find a tag
	tags, err := reader.Inventory()
	if err != nil || len(tags) == 0 {
		t.Skip("No tags in reader field")
	}

	server := NewHttpServer("", reader, false)

	// Set AFI to DA (secure)
	sid := tags[0]
	req := httptest.NewRequest("GET", fmt.Sprintf("/secure?%s=DA", sid), nil)
	w := httptest.NewRecorder()
	server.handleSecure(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify AFI
	afi, err := reader.ReadAfi(sid)
	if err != nil {
		t.Fatalf("ReadAfi failed: %v", err)
	}
	if afi != 0xDA {
		t.Errorf("AFI = %02x, want DA", afi)
	}
}

func TestIntegrationHandleProgram(t *testing.T) {
	skipIfNoHardware(t)
	reader := openReader(t)
	defer reader.Close()

	// Find a tag
	tags, err := reader.Inventory()
	if err != nil || len(tags) == 0 {
		t.Skip("No tags in reader field")
	}

	server := NewHttpServer("", reader, false)

	sid := tags[0]
	req := httptest.NewRequest("GET", fmt.Sprintf("/program?%s=TestIntHTTP", sid), nil)
	w := httptest.NewRecorder()
	server.handleProgram(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json parse error: %v", err)
	}
	okVal, ok := resp["ok"].(float64)
	if !ok {
		t.Fatalf("missing 'ok' in response")
	}
	if int(okVal) != 1 {
		t.Errorf("ok = %d, want 1", int(okVal))
	}
}

// ---------------------------------------------------------------------------
// Helper to run integration tests with logging

func TestMain(m *testing.M) {
	port := os.Getenv("RFID_PORT")
	if port != "" {
		log.Printf("RFID_PORT=%s — integration tests will use real hardware", port)
	}
	os.Exit(m.Run())
}
