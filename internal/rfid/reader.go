package rfid

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// 3M 810 RFID Reader Protocol Implementation
// Based on reverse engineering in Biblio::RFID::Reader::3M810
// Verified against Perl implementation: CRC table matches probe vectors

const (
	ReaderBaud  = 19200
	FramePrefix = 0xD6
	ProbePrefix = 0xD5
)

var crcTable [256]uint16

func init() {
	// Modified CCITT CRC-16: poly=0x1021, init=0xFFFF, xorout=0xFFFF, refin=false, refout=false
	poly := uint16(0x1021)
	for i := 0; i < 256; i++ {
		crc := uint16(i << 8)
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
		crcTable[i] = crc & 0xFFFF
	}
}

func crc16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc = (crc << 8) ^ crcTable[(uint8(crc>>8)^b)&0xFF]
	}
	crc ^= 0xFFFF
	return crc
}

// RfidReader wraps serial port communication with 3M 810 reader
type RfidReader struct {
	port     serial.Port
	debug    bool
	portName string       // saved port name for re-connect on reset
	mu       sync.Mutex   // guards serial port access against concurrent HTTP requests
}

func NewRfidReader(comPort string, debug bool) (*RfidReader, error) {
	mode := &serial.Mode{
		BaudRate: ReaderBaud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(comPort, mode)
	if err != nil {
		return nil, fmt.Errorf("open serial port: %w", err)
	}
	port.SetReadTimeout(100 * time.Millisecond)

	r := &RfidReader{port: port, debug: debug, portName: comPort}

	// Drain any pending data on startup (matches Perl init)
	buf := make([]byte, 3)
	n, _ := port.Read(buf)
	if n > 0 {
		lenByte := buf[2]
		drain := make([]byte, lenByte)
		port.Read(drain)
		if debug {
			log.Printf("drain: %x", drain)
		}
	}

	return r, nil
}

// Close closes the serial port
func (r *RfidReader) Close() {
	r.port.Close()
}

// sendFrame sends a command frame to the reader.
// If hex starts with D5/D6, send as-is; otherwise wrap with length prefix + CRC.
func (r *RfidReader) sendFrame(hexCmd string) error {
	data, err := hex.DecodeString(strings.ReplaceAll(hexCmd, " ", ""))
	if err != nil {
		return fmt.Errorf("hex decode: %w", err)
	}

	if data[0] != FramePrefix && data[0] != ProbePrefix {
		// Wrap with length prefix (2-byte big-endian) and CRC
		lenBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBytes, uint16(len(data)+2))
		payload := append(lenBytes, data...)
		csum := crc16(payload)
		cksumBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(cksumBytes, csum)
		data = append([]byte{FramePrefix}, payload...)
		data = append(data, cksumBytes...)
	}

	if r.debug {
		log.Printf(">> %x", data)
	}

	_, err = r.port.Write(data)
	return err
}

// readResponse reads a framed response from the reader.
func (r *RfidReader) readResponse() ([]byte, error) {
	// Read header: 3 bytes (prefix + 2-byte big-endian length)
	header := make([]byte, 3)
	pos := 0
	for pos < 3 {
		n, err := r.port.Read(header[pos:])
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		pos += n
	}

	// Accept any prefix byte
	prefix := header[0]
	_ = prefix

	// Length is 2-byte big-endian value
	payloadLen := int(binary.BigEndian.Uint16(header[1:3]))

	if payloadLen < 2 {
		return nil, fmt.Errorf("short response length %d", payloadLen)
	}

	// Read payload (includes 2-byte CRC at end)
	payload := make([]byte, payloadLen)
	pos = 0
	for pos < payloadLen {
		n, err := r.port.Read(payload[pos:])
		if err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
		pos += n
	}

	// Verify CRC
	crcHeader := header[1:3]
	crcPayload := payload[:payloadLen-2]
	crcInput := append(crcHeader, crcPayload...)
	expectedCRC := binary.BigEndian.Uint16(payload[payloadLen-2 : payloadLen])
	computedCRC := crc16(crcInput)
	if computedCRC != expectedCRC {
		if r.debug {
			log.Printf("CRC mismatch: computed %04x != expected %04x (prefix %02x)", computedCRC, expectedCRC, prefix)
		}
		return nil, fmt.Errorf("CRC mismatch: computed %04x != expected %04x", computedCRC, expectedCRC)
	}

	if r.debug {
		log.Printf("<< %02x %02x %02x %x", prefix, header[1], header[2], payload[:payloadLen-2])
	}

	return payload[:payloadLen-2], nil
}

// Reset closes and re-opens the serial port to recover a stuck reader.
func (r *RfidReader) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.debug {
		log.Printf("resetting serial port %s", r.portName)
	}
	r.port.Close()

	mode := &serial.Mode{
		BaudRate: ReaderBaud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(r.portName, mode)
	if err != nil {
		return fmt.Errorf("re-open serial port: %w", err)
	}
	port.SetReadTimeout(100 * time.Millisecond)
	r.port = port

	// Drain pending data
	buf := make([]byte, 3)
	n, _ := port.Read(buf)
	if n > 0 {
		lenByte := buf[2]
		drain := make([]byte, lenByte)
		port.Read(drain)
		if r.debug {
			log.Printf("drain after reset: %x", drain)
		}
	}

	if r.debug {
		log.Printf("serial port %s reset OK", r.portName)
	}
	return nil
}

// Lock / Unlock for concurrent access from HTTP handlers
func (r *RfidReader) Lock() {
	r.mu.Lock()
}

func (r *RfidReader) Unlock() {
	r.mu.Unlock()
}

// Probe sends the hardware version probe command.
func (r *RfidReader) Probe() (string, error) {
	err := r.sendFrame("D5 00 05 04 00 11 8C66")
	if err != nil {
		return "", err
	}
	resp, err := r.readResponse()
	if err != nil {
		return "", err
	}
	if len(resp) < 7 {
		return "", fmt.Errorf("short probe response: %x", resp)
	}
	if resp[0] != 0x04 || resp[1] != 0x00 || resp[2] != 0x11 {
		return "", fmt.Errorf("unexpected probe response prefix: %x", resp[:3])
	}
	hwVer := resp[3:7]
	return fmt.Sprintf("%d.%d.%d.%d", hwVer[0], hwVer[1], hwVer[2], hwVer[3]), nil
}

// Inventory scans for all tags in reader range.
func (r *RfidReader) Inventory() ([]string, error) {
	err := r.sendFrame("FE 00 05")
	if err != nil {
		return nil, err
	}
	resp, err := r.readResponse()
	if err != nil {
		return nil, err
	}
	if len(resp) < 5 {
		return nil, fmt.Errorf("short inventory response: %x", resp)
	}
	if resp[0] != 0xFE || resp[1] != 0x00 || resp[2] != 0x00 || resp[3] != 0x05 {
		return nil, fmt.Errorf("unexpected inventory header: %x", resp[:4])
	}
	nrTags := int(resp[4])
	if nrTags == 0 {
		return nil, nil
	}
	tagData := resp[5:]
	if len(tagData) < nrTags*8 {
		return nil, fmt.Errorf("inventory data too short: %d bytes for %d tags", len(tagData), nrTags)
	}
	var tags []string
	for i := 0; i < nrTags; i++ {
		tagBytes := tagData[i*8 : i*8+8]
		tags = append(tags, hex.EncodeToString(tagBytes))
	}
	return tags, nil
}

// consecutiveFailures tracks serial errors for auto-reset detection.
var consecutiveFailures int

// InventoryWithReset performs Inventory with automatic serial port reset when
// the reader stops responding (consecutive failures).  Callers should use this
// instead of Inventory to recover from a stuck reader automatically.
func (r *RfidReader) InventoryWithReset() ([]string, error) {
	// Try the normal scan first
	tags, err := r.Inventory()
	if err == nil {
		consecutiveFailures = 0
		return tags, nil
	}

	consecutiveFailures++
	if r.debug {
		log.Printf("inventory error (consecutive failures: %d): %v", consecutiveFailures, err)
	}

	// After 3 consecutive failures, reset the serial port
	if consecutiveFailures >= 3 {
		if r.debug {
			log.Printf("resetting serial port after %d consecutive failures", consecutiveFailures)
		}
		if resetErr := r.Reset(); resetErr != nil {
			log.Printf("serial port reset failed: %v", resetErr)
		} else {
			consecutiveFailures = 0
			// Try once more after reset
			tags, err = r.Inventory()
			if err == nil {
				return tags, nil
			}
			if r.debug {
				log.Printf("inventory still failing after reset: %v", err)
			}
		}
	}

	return nil, err
}

// ReadBlocks reads blocks from a tag.
func (r *RfidReader) ReadBlocks(tag string, start, count int) (map[int]string, error) {
	if len(tag) != 16 {
		return nil, fmt.Errorf("tag must be 16 hex chars")
	}
	cmdHex := fmt.Sprintf("02 %s %02x %02x", tag, start, count)
	err := r.sendFrame(cmdHex)
	if err != nil {
		return nil, err
	}
	resp, err := r.readResponse()
	if err != nil {
		return nil, err
	}
	if len(resp) < 11 || resp[0] != 0x02 || resp[1] != 0x00 {
		return nil, fmt.Errorf("unexpected read response: %x", resp)
	}
	nrBlocks := int(resp[10])
	if nrBlocks == 0 {
		return nil, nil
	}
	blockData := resp[11:]
	blocks := make(map[int]string)
	for i := 0; i < nrBlocks; i++ {
		offset := i * 6
		if offset+6 > len(blockData) {
			break
		}
		blockNum := int(binary.LittleEndian.Uint16(blockData[offset : offset+2]))
		payload := blockData[offset+2 : offset+6]
		blocks[blockNum] = hex.EncodeToString(payload)
	}
	return blocks, nil
}

// WriteBlocks writes data to a tag with verification (read-back + retry).
func (r *RfidReader) WriteBlocks(tag string, data string) error {
	if len(tag) != 16 {
		return fmt.Errorf("tag must be 16 hex chars")
	}
	dataBytes, err := hex.DecodeString(strings.ReplaceAll(data, " ", ""))
	if err != nil {
		return fmt.Errorf("data hex decode: %w", err)
	}
	if len(dataBytes)%4 != 0 {
		pad := make([]byte, 4-len(dataBytes)%4)
		dataBytes = append(dataBytes, pad...)
	}
	nrBlocks := len(dataBytes) / 4
	cmdHex := fmt.Sprintf("04 %s 00 %02x 00 %s", tag, nrBlocks, hex.EncodeToString(dataBytes))

	err = r.sendFrame(cmdHex)
	if err != nil {
		return err
	}
	resp, err := r.readResponse()
	if err != nil {
		return err
	}
	if len(resp) < 2 || resp[0] != 0x04 || resp[1] != 0x00 {
		return fmt.Errorf("write failed: %x", resp)
	}

	// Verify by reading back (retry up to 10 times like Perl)
	maxRetry := 10
	for retry := 0; retry < maxRetry; retry++ {
		verifyBlocks, err := r.ReadBlocks(tag, 0, 8)
		if err != nil {
			if retry < maxRetry-1 {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return fmt.Errorf("write verify failed: %w", err)
		}
		// Check that written data matches
		written := make([]byte, 0, len(dataBytes))
		for bi := 0; bi < nrBlocks; bi++ {
			if hexStr, ok := verifyBlocks[bi]; ok {
				b, _ := hex.DecodeString(hexStr)
				written = append(written, b...)
			}
		}
		if len(written) == len(dataBytes) {
			match := true
			for i := 0; i < len(dataBytes); i++ {
				if written[i] != dataBytes[i] {
					match = false
					break
				}
			}
			if match {
				return nil
			}
		}
		if r.debug {
			log.Printf("write verify mismatch, retry %d/%d", retry+1, maxRetry)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("write verification failed after %d retries", maxRetry)
}

// ReadAfi reads the AFI byte from a tag.
func (r *RfidReader) ReadAfi(tag string) (byte, error) {
	if len(tag) != 16 {
		return 0, fmt.Errorf("tag must be 16 hex chars")
	}
	cmdHex := fmt.Sprintf("0A %s", tag)
	err := r.sendFrame(cmdHex)
	if err != nil {
		return 0, err
	}
	resp, err := r.readResponse()
	if err != nil {
		return 0, err
	}
	if len(resp) < 11 || resp[0] != 0x0A || resp[1] != 0x00 {
		return 0, fmt.Errorf("read AFI failed: %x", resp)
	}
	afi := resp[10]
	return afi, nil
}

// WriteAfi writes an AFI byte to a tag with retry loop and read-back verification.
func (r *RfidReader) WriteAfi(tag string, afi byte) error {
	if len(tag) != 16 {
		return fmt.Errorf("tag must be 16 hex chars")
	}
	cmdHex := fmt.Sprintf("09 %s %02x", tag, afi)
	maxRetry := 100
	var lastErr error
	for i := 0; i < maxRetry; i++ {
		err := r.sendFrame(cmdHex)
		if err != nil {
			lastErr = fmt.Errorf("send: %w", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		resp, err := r.readResponse()
		if err != nil {
			// CRC errors are retryable — reader may need recovery
			if r.debug {
				log.Printf("write AFI read error (retry %d): %v", i+1, err)
			}
			lastErr = fmt.Errorf("read: %w", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if len(resp) >= 2 && resp[0] == 0x09 && resp[1] == 0x00 {
			// Reader accepted the write — now verify by reading back AFI
			readAfi, err := r.ReadAfi(tag)
			if err != nil {
				if r.debug {
					log.Printf("write AFI verify read error (retry %d): %v", i+1, err)
				}
				lastErr = fmt.Errorf("verify read: %w", err)
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if readAfi == afi {
				return nil
			}
			if r.debug {
				log.Printf("write AFI verify mismatch: wrote %02x, read back %02x (retry %d/%d)", afi, readAfi, i+1, maxRetry)
			}
			lastErr = fmt.Errorf("verify mismatch: wrote %02x, read back %02x", afi, readAfi)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if len(resp) >= 2 && resp[0] == 0x09 && resp[1] == 0x06 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return fmt.Errorf("unexpected AFI write response: %x", resp)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("AFI write max retries exceeded")
}

// AFI constants for library items
const (
	AfiSecure   byte = 0xDA // checked in (secure) – door will ignore
	AfiUnsecure byte = 0xD7 // checked out (unsecure) – door will beep
)
