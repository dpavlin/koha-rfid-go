package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"go.bug.st/serial"
)

// 3M 810 RFID Reader Protocol Implementation
// Based on reverse engineering in Biblio::RFID::Reader::3M810

const (
	ReaderBaud  = 19200
	FramePrefix = 0xD6
	ProbePrefix = 0xD5
)

var crcTable [256]uint16

func init() {
	// CCITT CRC-16 with poly 0x1021, init 0xFFFF, xorout 0xFFFF, refin=false, refout=false
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
	port  serial.Port
	debug bool
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

	r := &RfidReader{port: port, debug: debug}

	// Drain any pending data on startup
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
// If hex starts with D5/D6, send as-is; otherwise wrap with length+CRC.
func (r *RfidReader) sendFrame(hexCmd string) error {
	data, err := hex.DecodeString(strings.ReplaceAll(hexCmd, " ", ""))
	if err != nil {
		return fmt.Errorf("hex decode: %w", err)
	}

	if data[0] != FramePrefix && data[0] != ProbePrefix {
		// Wrap with length prefix and CRC
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

// readResponse reads a response from the reader.
// Returns the payload (after length byte and CRC checked).
func (r *RfidReader) readResponse() ([]byte, error) {
	// Read header bytes
	header := make([]byte, 3)
	// Read until we have at least 3 bytes
	pos := 0
	for pos < 3 {
		n, err := r.port.Read(header[pos:])
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		pos += n
	}

	if header[0] != FramePrefix && header[0] != ProbePrefix {
		return nil, fmt.Errorf("unexpected prefix: %02x", header[0])
	}

	var payload []byte
	if header[0] == ProbePrefix {
		// D5 format: D5 00 <len1> <data...> <crc16>
		payloadLen := int(header[2])
		payload = make([]byte, payloadLen)
		pos := 0
		for pos < payloadLen {
			n, err := r.port.Read(payload[pos:])
			if err != nil {
				return nil, fmt.Errorf("read payload: %w", err)
			}
			pos += n
		}
		// CRC is the last 2 bytes; strip it
		if len(payload) >= 2 {
			payload = payload[:len(payload)-2]
		}
	} else {
		// D6 format: D6 <len2 big-endian> <data...> <crc16>
		lenBytes := make([]byte, 2)
		lenBytes[0] = header[1]
		lenBytes[1] = header[2]
		payloadLen := int(binary.BigEndian.Uint16(lenBytes)) - 2 // minus CRC
		payload = make([]byte, payloadLen)
		pos := 0
		for pos < payloadLen {
			n, err := r.port.Read(payload[pos:])
			if err != nil {
				return nil, fmt.Errorf("read payload: %w", err)
			}
			pos += n
		}
	}

	if r.debug {
		log.Printf("<< %x", payload)
	}
	return payload, nil
}

// Probe sends the hardware version probe command
func (r *RfidReader) Probe() (string, error) {
	err := r.sendFrame("D5 00 05 04 00 11 8C66")
	if err != nil {
		return "", err
	}
	resp, err := r.readResponse()
	if err != nil {
		return "", err
	}
	// Expected: D5 00 09 04 00 11 <hw_ver_4_bytes> <crc>
	if len(resp) < 7 {
		return "", fmt.Errorf("short probe response: %x", resp)
	}
	// Verify response prefix
	expectedPrefix := []byte{0x04, 0x00, 0x11}
	if len(resp) < 3 || resp[0] != expectedPrefix[0] || resp[1] != expectedPrefix[1] || resp[2] != expectedPrefix[2] {
		return "", fmt.Errorf("unexpected probe response: %x", resp)
	}
	hwVer := resp[3:7]
	return fmt.Sprintf("%d.%d.%d.%d", hwVer[0], hwVer[1], hwVer[2], hwVer[3]), nil
}

// Inventory scans for all tags in range. Returns list of 8-byte hex tag IDs.
func (r *RfidReader) Inventory() ([]string, error) {
	err := r.sendFrame("FE 00 05")
	if err != nil {
		return nil, err
	}
	resp, err := r.readResponse()
	if err != nil {
		return nil, err
	}
	// Expected: FE 00 00 05 <nr_tags_1> <tag1_8bytes> <tag2_8bytes> ...
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
	if len(tagData) != nrTags*8 {
		return nil, fmt.Errorf("inventory data length mismatch: %d bytes for %d tags", len(tagData), nrTags)
	}
	var tags []string
	for i := 0; i < nrTags; i++ {
		tagBytes := tagData[i*8 : i*8+8]
		tags = append(tags, hex.EncodeToString(tagBytes))
	}
	return tags, nil
}

// ReadBlocks reads blocks from a tag. Returns map of block number -> 4-byte hex payload.
func (r *RfidReader) ReadBlocks(tag string, start, count int) (map[int]string, error) {
	if len(tag) != 16 {
		return nil, fmt.Errorf("tag must be 16 hex chars (8 bytes)")
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
	// Response: 02 00 <tag8> <nr_blocks_1> <block_data...>
	if len(resp) < 11 {
		return nil, fmt.Errorf("short read response: %x", resp)
	}
	if resp[0] != 0x02 || resp[1] != 0x00 {
		return nil, fmt.Errorf("unexpected read header: %x", resp[:2])
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

// WriteBlocks writes data to a tag. Data is a hex string of 4-byte blocks.
func (r *RfidReader) WriteBlocks(tag string, data string) error {
	if len(tag) != 16 {
		return fmt.Errorf("tag must be 16 hex chars")
	}
	dataBytes, err := hex.DecodeString(strings.ReplaceAll(data, " ", ""))
	if err != nil {
		return fmt.Errorf("data hex decode: %w", err)
	}
	if len(dataBytes)%4 != 0 {
		// Pad to multiple of 4
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
	// Verify by reading back (optional; could add retry loop like Perl code)
	return nil
}

// ReadAfi reads the AFI byte from a tag. Returns AFI byte.
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

// WriteAfi writes an AFI byte to a tag.
func (r *RfidReader) WriteAfi(tag string, afi byte) error {
	if len(tag) != 16 {
		return fmt.Errorf("tag must be 16 hex chars")
	}
	cmdHex := fmt.Sprintf("09 %s %02x", tag, afi)
	// Retry loop like Perl code
	maxRetry := 100
	for i := 0; i < maxRetry; i++ {
		err := r.sendFrame(cmdHex)
		if err != nil {
			return err
		}
		resp, err := r.readResponse()
		if err != nil {
			return err
		}
		if len(resp) >= 2 && resp[0] == 0x09 && resp[1] == 0x00 {
			return nil
		}
		// If error (09 06), retry
		if len(resp) >= 2 && resp[0] == 0x09 && resp[1] == 0x06 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return fmt.Errorf("unexpected AFI write response: %x", resp)
	}
	return fmt.Errorf("AFI write max retries exceeded")
}

// AFI constants for library items
const (
	AfiSecure   byte = 0xDA // checked in (secure)
	AfiUnsecure byte = 0xD7 // checked out (unsecure)
)
