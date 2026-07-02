package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"koha-rfid/internal/rfid"
)

// scan.pl equivalent: CLI tool that continuously scans RFID tags and prints
// ISO date, tag SID, AFI, and RFID501 decoded content.
//
// Usage:
//   scan -port /dev/ttyUSB0 [-debug] [-loop] [-log file.csv]
//
// The -loop flag runs continuously with enter/leave detection.
// The -log flag appends ISO date,tag,content to a CSV file on first detection.

func main() {
	comPort := flag.String("port", "/dev/ttyUSB0", "Serial port for 3M RFID reader")
	debug := flag.Bool("debug", false, "Enable debug logging")
	loop := flag.Bool("loop", false, "Continuously scan (default: one-shot)")
	logFile := flag.String("log", "", "CSV log file path for tag appearances")
	flag.Parse()

	reader, err := rfid.NewRfidReader(*comPort, *debug)
	if err != nil {
		log.Fatalf("RFID reader: %v", err)
	}
	defer reader.Close()

	hwVer, err := reader.Probe()
	if err != nil {
		log.Fatalf("RFID probe failed: %v", err)
	}
	log.Printf("3M 810 hardware version: %s", hwVer)

	// Track previously-seen tags for enter/leave detection
	prevTags := make(map[string]bool)
	var logFh *os.File

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("open log %s: %v", *logFile, err)
		}
		logFh = f
		defer f.Close()
	}

	isoDate := func() string {
		now := time.Now()
		return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d",
			now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second())
	}

	// Handle SIGINT for clean exit in loop mode
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	done := false
	for !done {
		select {
		case <-sig:
			done = true
		default:
		}

		tags, err := reader.Inventory()
		if err != nil {
			log.Printf("Scan error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Build set of current tags
		current := make(map[string]bool)
		for _, t := range tags {
			current[t] = true
		}

		// For each tag present, read AFI and blocks, print details
		for _, tag := range tags {
			afi, err := reader.ReadAfi(tag)
			afiStr := ""
			if err == nil {
				afiStr = fmt.Sprintf("%02X", afi)
			}

			// Decode RFID501
			blocks, err := reader.ReadBlocks(tag, 0, 8)
			content := ""
			var decoded *rfid.RFID501Tag
			if err == nil && len(blocks) > 0 {
				blockHexes := make([]string, len(blocks))
				for i := 0; i < len(blocks); i++ {
					if b, ok := blocks[i]; ok {
						blockHexes[i] = b
					}
				}
				decoded = rfid.DecodeRFID501(blockHexes)
				if decoded != nil {
					content = decoded.Content
				} else {
					if b0, ok := blocks[0]; ok {
						bb, _ := hexDecodeString(b0)
						content = string(bb)
					}
				}
			}

			// Enter detection: print when tag was NOT in previous scan
			if !prevTags[tag] {
				fmt.Printf("%s reader 3M810 enter %s AFI: %s %s\n",
					isoDate(), strings.ToUpper(tag), afiStr, formatDecoded(decoded))

				// Log to CSV on first appearance
				if logFh != nil && content != "" {
					fmt.Fprintf(logFh, "%s,%s,%s\n", isoDate(), strings.ToUpper(tag), content)
				}
			}

			// Mark as seen in this scan
			prevTags[tag] = true
		}

		// Leave detection: tags that were in prevTags but not in current
		for tag := range prevTags {
			if !current[tag] {
				fmt.Printf("%s leave %s\n", isoDate(), strings.ToUpper(tag))
				delete(prevTags, tag)
			}
		}

		// Print visible tags summary
		if len(tags) > 0 {
			fmt.Printf("%s visible: %s\n", isoDate(), strings.Join(tags, " "))
		} else {
			fmt.Printf("%s visible: \n", isoDate())
		}

		if !*loop {
			break
		}
		time.Sleep(1 * time.Second)
	}
}

func formatDecoded(d *rfid.RFID501Tag) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("{ content => %q, type => %d (%s), set => %d, total => %d, branch => %d, library => %d, custom => %d }",
		d.Content, d.Type, d.TypeLabel, d.Set, d.Total, d.Branch, d.Library, d.Custom)
}

func hexDecodeString(s string) ([]byte, error) {
	buf := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		v, ok := fromHexChar(s[i])
		if !ok {
			return nil, fmt.Errorf("invalid hex char: %c", s[i])
		}
		buf[i/2] = v << 4
		v, ok = fromHexChar(s[i+1])
		if !ok {
			return nil, fmt.Errorf("invalid hex char: %c", s[i+1])
		}
		buf[i/2] |= v
	}
	return buf, nil
}

func fromHexChar(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
