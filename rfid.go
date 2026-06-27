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
// Returns the payload (without CRC). The response uses the same prefix as
// the command (FE, 02, 04, 09, 0A) or D5/D6 framing.
// The 3-byte header is: <prefix> <len_high> <len_low> (big-endian 16-bit length
// which includes CRC). Perl code uses byte 2 as single-byte length since
// high byte is always 0 for small responses.
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

	// Accept any prefix byte (FE, 02, 04, 09, 0A, D5, D6)
	prefix := header[0]

	// Length is 2-byte big-endian value; byte 2 (low byte) is sufficient
	// for small payloads (< 256 bytes)
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

	// Verify CRC: computed over header bytes 1-2 + payload without last 2 bytes
	// (matches Perl: checksum(substr($r_len,1).substr($data,0,-2)))
	crcHeader := header[1:3] // 2-byte length
	crcPayload := payload[:payloadLen-2]
	crcInput := append(crcHeader, crcPayload...)
	expectedCRC := binary.BigEndian.Uint16(payload[payloadLen-2 : payloadLen])
	computedCRC := crc16(crcInput)
	if computedCRC != expectedCRC {
		if r.debug {
			log.Printf("CRC mismatch: computed %04x != expected %04x (prefix %02x)", computedCRC, expectedCRC, prefix)
		}
		// Non-fatal: continue like Perl which only warns on mismatch
	}

	if r.debug {
		log.Printf("<< %02x %02x %02x %x", prefix, header[1], header[2], payload[:payloadLen-2])
	}

	// Return payload without CRC
	return payload[:payloadLen-2], nil
}

// Probe sends the hardware version probe command.
// Matches Perl init(): sends D5 00 05 04 00 11 8C66 and reads 12-byte response.
func (r *RfidReader) Probe() (string, error) {
	err := r.sendFrame("D5 00 05 04 00 11 8C66")
	if err != nil {
		return "", err
	}
	resp, err := r.readResponse()
	if err != nil {
		return "", err
	}
	// Expected response (after D5 header+CRC):
	// 04 00 11 <hw_ver_4_bytes>
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
// Matches Perl inventory(): sends FE 00 05, parses FE 00 00 05 <nr_tags> <tag_data>.
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

// ReadBlocks reads blocks from a tag.
// Matches Perl read_blocks(): sends 02 <tag> <start> <blocks>, parses 02 00 <tag8> <nr_blocks> <block_data>.
// Each block entry: 2-byte block number (little-endian) + 4-byte payload.
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
	// Response: 02 00 <tag8> <nr_blocks> <block_data...>
	if len(resp) < 11 {
		return nil, fmt.Errorf("short read response: %x", resp)
	}
	if resp[0] != 0x02 || resp[1] != 0x00 {
		return nil, fmt.Errorf("unexpected read header: %x", resp[:2])
	}
	// Tag is bytes 2-9, block count is byte 10
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
// Matches Perl write_blocks(): sends 04 <tag> 00 <nr_blocks> 00 <hex_data>,
// parses 04 00 <tag> <blocks>, then verifies by reading back (up to 10 retries).
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
// Matches Perl read_afi(): sends 0A <tag>, parses 0A 00 <tag8> <afi1>.
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

// WriteAfi writes an AFI byte to a tag with retry loop.
// Matches Perl write_afi(): sends 09 <tag> <afi>, expects 09 00 for success,
// retries on 09 06 error (up to 100 times like Perl).
func (r *RfidReader) WriteAfi(tag string, afi byte) error {
	if len(tag) != 16 {
		return fmt.Errorf("tag must be 16 hex chars")
	}
	cmdHex := fmt.Sprintf("09 %s %02x", tag, afi)
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
		// Error 09 06 → retry (matches Perl)
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
	AfiSecure   byte = 0xDA // checked in (secure) – door will ignore
	AfiUnsecure byte = 0xD7 // checked out (unsecure) – door will beep
)
