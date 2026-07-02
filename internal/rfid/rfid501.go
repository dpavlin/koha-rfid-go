package rfid

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// RFID501 implements the 3M RFID Standard for Libraries tag format.
//
// The tag uses 8 blocks × 4 bytes = 32 bytes total:
//
//   Block 0: [04] [set/total nibbles] [00] [item type]
//     byte 0: 0x04 fixed marker
//     byte 1: high nibble = index in set, low nibble = total items
//     byte 2: 0x00 reserved
//     byte 3: item type code
//
//   Blocks 1-4: 16-byte null-padded barcode (ASCII, right-padded with \x00)
//
//   Block 5: 4 bytes: branch(12 bits) + library(20 bits), big-endian unsigned
//
//   Block 6: 4 bytes: custom signed integer, big-endian
//
//   Block 7: 4 bytes: zero (must be \x00\x00\x00\x00)
//
// Reference: Biblio::RFID::RFID501 (Perl), 3M document "RFID 501"

// ItemType codes (from Perl %item_type)
const (
	ItemTypeOther         = 0
	ItemTypeBook          = 1
	ItemTypeMagazine      = 2
	ItemTypeBoundJournal  = 3
	ItemTypeAudioTape     = 4
	ItemTypeVideo         = 5
	ItemTypeCD            = 6
	ItemTypeDiskette      = 7
	ItemTypeBookWithDisk  = 8
	ItemTypeBookWithCD    = 9
	ItemTypeBookWithAudio = 13
)

var itemTypeLabels = map[byte]string{
	ItemTypeOther:         "Other",
	ItemTypeBook:          "Book",
	ItemTypeMagazine:      "Magazine",
	ItemTypeBoundJournal:  "Bound Journal",
	ItemTypeAudioTape:     "Audio Tape",
	ItemTypeVideo:         "Video",
	ItemTypeCD:            "CD/CD ROM",
	ItemTypeDiskette:      "Diskette",
	ItemTypeBookWithDisk:  "Book with Diskette",
	ItemTypeBookWithCD:    "Book with CD/CD ROM",
	ItemTypeBookWithAudio: "Book with Audio Tape",
}

// RFID501Tag holds the decoded fields from an RFID501 tag.
type RFID501Tag struct {
	U1        byte   // should be 0x04
	Set       int    // index in set (0-15)
	Total     int    // total items in set (0-15)
	U2        byte   // should be 0x00
	Type      byte   // item type code
	TypeLabel string // human-readable type
	Content   string // barcode (null-padded ASCII, trimmed)
	Branch    int    // branch number (12 bits, 0-4095)
	Library   int    // library number (20 bits, 0-1048575)
	Custom    int32  // custom signed integer
}

// DecodeRFID501 decodes 8 blocks (each 4-byte hex) into a structured tag.
// Accepts []string of hex blocks, [][]byte, or a single []byte payload.
// Returns nil if data is too short (< 24 bytes).
func DecodeRFID501(blocks interface{}) *RFID501Tag {
	var data []byte

	switch v := blocks.(type) {
	case []string:
		for _, h := range v {
			cleaned := strings.ReplaceAll(h, " ", "")
			b, err := hex.DecodeString(cleaned)
			if err != nil {
				return nil
			}
			if len(b) != 4 {
				return nil
			}
			data = append(data, b...)
		}
	case [][]byte:
		for _, b := range v {
			if len(b) != 4 {
				return nil
			}
			data = append(data, b...)
		}
	case []byte:
		data = v
	default:
		return nil
	}

	if len(data) < 24 {
		return nil
	}
	// Use at most 32 bytes (8 blocks)
	if len(data) > 32 {
		data = data[:32]
	}
	// Pad to 32 bytes if needed
	if len(data) < 32 {
		pad := make([]byte, 32-len(data))
		data = append(data, pad...)
	}

	tag := &RFID501Tag{}
	tag.U1 = data[0]
	tag.Set = int(data[1] >> 4)
	tag.Total = int(data[1] & 0x0f)
	tag.U2 = data[2]
	tag.Type = data[3]

	if label, ok := itemTypeLabels[tag.Type]; ok {
		tag.TypeLabel = label
	} else {
		tag.TypeLabel = fmt.Sprintf("Unknown(%d)", tag.Type)
	}

	// Extract null-padded barcode (bytes 4-19, Z16 format)
	barcodeBytes := data[4:20]
	// Find first null byte to trim
	nullIdx := 16
	for i, b := range barcodeBytes {
		if b == 0 {
			nullIdx = i
			break
		}
	}
	tag.Content = string(barcodeBytes[:nullIdx])

	// Block 5: branch(12 bits) + library(20 bits), big-endian
	if len(data) >= 24 {
		brLib := binary.BigEndian.Uint32(data[20:24])
		tag.Branch = int(brLib >> 20)
		tag.Library = int(brLib & 0x000FFFFF)
	}

	// Block 6: custom signed integer, big-endian
	if len(data) >= 28 {
		tag.Custom = int32(binary.BigEndian.Uint32(data[24:28]))
	}

	return tag
}

// EncodeRFID501 encodes tag fields into 8 blocks of 4-byte hex strings.
// Returns 8 hex strings ready for WriteBlocks.
func EncodeRFID501(tag *RFID501Tag) []string {
	data := make([]byte, 32)

	data[0] = 0x04
	data[1] = byte((tag.Set << 4) | (tag.Total & 0x0f))
	data[2] = 0x00
	data[3] = tag.Type

	// Barcode: copy up to 16 bytes, null-pad
	contentBytes := []byte(tag.Content)
	if len(contentBytes) > 16 {
		contentBytes = contentBytes[:16]
	}
	copy(data[4:20], contentBytes)
	// Remaining bytes in data[4:20] are already zero from make()

	brLib := uint32(tag.Branch)<<20 | uint32(tag.Library&0x000FFFFF)
	binary.BigEndian.PutUint32(data[20:24], brLib)

	binary.BigEndian.PutUint32(data[24:28], uint32(tag.Custom))

	// Block 7: zero (already zero from make())

	// Split into 8 × 4-byte hex strings
	blocks := make([]string, 8)
	for i := 0; i < 8; i++ {
		blocks[i] = hex.EncodeToString(data[i*4 : i*4+4])
	}
	return blocks
}

// EncodeRFID501Content encodes content into a full 8-block hex string for WriteBlocks.
// Detects item type based on barcode prefix (130xxx = Book).
func EncodeRFID501Content(content string) string {
	itemType := byte(ItemTypeBook)
	if len(content) >= 3 && content[:3] == "130" {
		itemType = ItemTypeBook
	} else {
		itemType = ItemTypeOther
	}

	tag := &RFID501Tag{
		Set:      1,
		Total:    1,
		Type:     itemType,
		Content:  content,
		Branch:   0,
		Library:  0,
		Custom:   0,
	}

	blocks := EncodeRFID501(tag)
	result := ""
	for _, b := range blocks {
		result += b
	}
	return result
}

// BlankRFID501 returns 3 blocks of zeros (generic blank tag).
func BlankRFID501() []string {
	blocks := make([]string, 3)
	for i := 0; i < 3; i++ {
		blocks[i] = "00000000"
	}
	return blocks
}

// Blank3MRFID501 returns 6 blocks of 0x55 + 1 block of zeros (3M blank).
func Blank3MRFID501() []string {
	blocks := make([]string, 7)
	for i := 0; i < 6; i++ {
		blocks[i] = "55555555"
	}
	blocks[6] = "00000000"
	return blocks
}
