// Package rfidops provides shared RFID operations used by both the HTTP server
// and CLI commands.  By factoring the RFID logic into a library package, each
// caller (server, scan CLI, program CLI) avoids duplicating the same scan /
// program / secure logic.
package rfidops

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"koha-rfid/internal/rfid"
)

// ErrReaderTimeout is returned when the RFID reader fails to respond.
var ErrReaderTimeout = errors.New("RFID reader timeout")

// RfidOps abstracts the RFID reader so callers can swap in a mock for testing.
type RfidOps interface {
	Inventory() ([]string, error)
	InventoryWithReset() ([]string, error) // auto-reset on consecutive failures
	ReadBlocks(tag string, start, count int) (map[int]string, error)
	WriteBlocks(tag string, data string) error
	ReadAfi(tag string) (byte, error)
	WriteAfi(tag string, afi byte) error
	Lock()
	Unlock()
}

// ---------------------------------------------------------------------------
// Shared operation functions
//
// Each function takes an RfidOps and returns structured result types.  No HTTP
// or terminal formatting here – just pure RFID logic.  Callers (HTTP handler,
// CLI command, test) are responsible for presentation.
// ---------------------------------------------------------------------------

// TagInfo holds decoded RFID tag data.
type TagInfo struct {
	SID      string `json:"sid"`
	Content  string `json:"content"`
	Security string `json:"security"`
	TagType  string `json:"tag_type"`
	Reader   string `json:"reader"`
}

// ScanResult is the structured outcome of a single scan pass.
type ScanResult struct {
	Tags []TagInfo
}

// Scan performs one inventory pass and returns decoded tag info for every tag
// found.  Uses InventoryWithReset for automatic reader recovery.
func Scan(ops RfidOps) (*ScanResult, error) {
	ops.Lock()
	defer ops.Unlock()

	tags, err := ops.InventoryWithReset()
	if err != nil {
		return nil, err
	}

	res := &ScanResult{}
	for _, tag := range tags {
		info := TagInfo{
			SID:     tag,
			TagType: "RFID501",
			Reader:  "3M810",
		}

		afi, err := ops.ReadAfi(tag)
		if err == nil {
			info.Security = strings.ToUpper(hex.EncodeToString([]byte{afi}))
		}

		blocks, err := ops.ReadBlocks(tag, 0, 8)
		if err == nil && len(blocks) > 0 {
			blockHexes := makeBlockHexes(blocks)
			if decoded := rfid.DecodeRFID501(blockHexes); decoded != nil {
				info.Content = decoded.Content
			} else if b0, ok := blocks[0]; ok {
				barcodeBytes, _ := hex.DecodeString(b0)
				info.Content = string(barcodeBytes)
			}
		}
		res.Tags = append(res.Tags, info)
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Program operations

// ProgramOp describes one tag to program.
type ProgramOp struct {
	SID     string // 16-char hex EPC
	Content string // barcode or "blank"
	// Optional custom RFID501 tag parameters (used when Content is a barcode).
	// If nil, Program uses rfid.EncodeRFID501Content(content) for auto-encoding.
	RFID501Tag *rfid.RFID501Tag
}

// ProgramResult reports outcome per tag.
type ProgramResult struct {
	OK     int      `json:"ok"`
	Errors []string // per-tag error messages; empty slice on full success
}

// Program writes one or more tags.
func Program(ops RfidOps, opsList []ProgramOp) *ProgramResult {
	ops.Lock()
	defer ops.Unlock()

	res := &ProgramResult{OK: 1}
	for _, op := range opsList {
		tag := strings.ToUpper(op.SID)
		content := op.Content

		if strings.ToLower(content) == "blank" {
			blocksHex := rfid.BlankRFID501()
			if err := ops.WriteBlocks(tag, blocksHex); err != nil {
				res.OK = 0
				res.Errors = append(res.Errors, err.Error())
				continue
			}
			if err := ops.WriteAfi(tag, rfid.AfiUnsecure); err != nil {
				res.OK = 0
				res.Errors = append(res.Errors, err.Error())
			}
			continue
		}

		var rfid501Hex string
		if op.RFID501Tag != nil {
			encoded := rfid.EncodeRFID501(op.RFID501Tag)
			for _, b := range encoded {
				rfid501Hex += b
			}
		} else {
			rfid501Hex = rfid.EncodeRFID501Content(content)
		}

		if err := ops.WriteBlocks(tag, rfid501Hex); err != nil {
			res.OK = 0
			res.Errors = append(res.Errors, err.Error())
			continue
		}

		afi := rfid.AfiUnsecure
		if strings.HasPrefix(content, "130") {
			afi = rfid.AfiSecure
		}
		if err := ops.WriteAfi(tag, afi); err != nil {
			res.OK = 0
			res.Errors = append(res.Errors, err.Error())
		}
	}
	return res
}

// ---------------------------------------------------------------------------
// Secure (AFI) operations

// SecureOp describes one tag to secure/unsecure.
type SecureOp struct {
	SID    string // 16-char hex EPC
	AfiHex string // 2-char hex AFI
}

// SecureResult reports outcome.
type SecureResult struct {
	OK    int    `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Secure writes AFI bytes to one or more tags.
func Secure(ops RfidOps, opsList []SecureOp) *SecureResult {
	ops.Lock()
	defer ops.Unlock()
	for _, op := range opsList {
		tag := strings.ToUpper(op.SID)
		afiByte, err := hex.DecodeString(op.AfiHex)
		if err != nil || len(afiByte) != 1 {
			return &SecureResult{OK: 0, Error: fmt.Sprintf("invalid AFI hex %s", op.AfiHex)}
		}
		if err := ops.WriteAfi(tag, afiByte[0]); err != nil {
			return &SecureResult{OK: 0, Error: err.Error()}
		}
	}
	return &SecureResult{OK: 1}
}

// ---------------------------------------------------------------------------
// Internal helpers

func makeBlockHexes(blocks map[int]string) []string {
	out := make([]string, len(blocks))
	for i := 0; i < len(blocks); i++ {
		if b, ok := blocks[i]; ok {
			out[i] = b
		}
	}
	return out
}
