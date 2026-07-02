package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"koha-rfid/internal/rfid"
)

// program.pl equivalent: CLI tool to program an RFID tag with content.
//
// Usage:
//   program -com COM3 [-debug] [-afi 214] [-type 1] [-set 1] [-total 1] E0_RFID_SID [barcode]
//   program -com COM3 -blank E0_RFID_SID
//   program -com COM3 -3mblank E0_RFID_SID
//
// The SID and barcode can also be comma-separated: program -com COM3 E0SID,barcode

func main() {
	comPort := flag.String("com", "COM3", "Serial port for 3M RFID reader")
	debug := flag.Bool("debug", false, "Enable debug logging")
	afi := flag.Int("afi", 0, "AFI byte value to write (e.g., 214 for secure, 218 for unsecure)")
	typeOpt := flag.Int("type", 0, "RFID501 item type (1=Book, 6=CD, etc.)")
	set := flag.Int("set", 1, "Set index (0-15)")
	total := flag.Int("total", 1, "Total items in set (0-15)")
	branch := flag.Int("branch", 0, "Branch number (12 bits, 0-4095)")
	library := flag.Int("library", 0, "Library number (20 bits, 0-1048575)")
	blank := flag.Bool("blank", false, "Write generic blank tag (3× zero blocks)")
	blank3M := flag.Bool("3mblank", false, "Write 3M manufacturing blank (6× 0x55 + zeros)")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s [options] E0_RFID_SID [barcode]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s [options] E0SID,barcode\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	sid := args[0]
	content := ""
	if len(args) >= 2 {
		content = args[1]
	}

	// Support comma-separated SID,content
	if strings.Contains(sid, ",") && content == "" {
		parts := strings.Split(sid, ",")
		if len(parts) >= 2 {
			sid = parts[0]
			content = parts[1]
		}
	}

	sid = strings.ToUpper(strings.TrimSpace(sid))
	if len(sid) != 16 {
		log.Fatalf("SID must be 16 hex chars, got %q (len=%d)", sid, len(sid))
	}

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

	// Scan for the target tag in inventory
	tags, err := reader.Inventory()
	if err != nil {
		log.Fatalf("Inventory scan failed: %v", err)
	}

	found := false
	for _, t := range tags {
		if strings.EqualFold(t, sid) {
			found = true
			break
		}
	}
	if !found {
		log.Printf("Warning: tag %s not found in inventory (will attempt anyway)", sid)
	}

	// Determine blank type
	if *blank {
		log.Printf("BLANK blank %s", sid)
		blocks := rfid.BlankRFID501()
		for _, block := range blocks {
			err := reader.WriteBlocks(sid, block)
			if err != nil {
				log.Fatalf("WriteBlocks blank: %v", err)
			}
		}
	} else if *blank3M {
		log.Printf("BLANK 3mblank %s", sid)
		blocks := rfid.Blank3MRFID501()
		for _, block := range blocks {
			err := reader.WriteBlocks(sid, block)
			if err != nil {
				log.Fatalf("WriteBlocks 3mblank: %v", err)
			}
		}
	} else if content != "" {
		log.Printf("PROGRAM %s with %s", sid, content)

		// Build RFID501 hash (matches Perl's from_hash)
		itemType := byte(*typeOpt)
		if itemType == 0 {
			// Auto-detect: books (130 prefix) get type 1
			if strings.HasPrefix(content, "130") {
				itemType = rfid.ItemTypeBook
			} else {
				itemType = rfid.ItemTypeOther
			}
		}

		tag := &rfid.RFID501Tag{
			Set:     *set,
			Total:   *total,
			Type:    itemType,
			Content: content,
			Branch:  *branch,
			Library: *library,
			Custom:  0,
		}

		encoded := rfid.EncodeRFID501(tag)
		fullHex := ""
		for _, b := range encoded {
			fullHex += b
		}
		err := reader.WriteBlocks(sid, fullHex)
		if err != nil {
			log.Fatalf("WriteBlocks: %v", err)
		}
	} else {
		log.Fatalf("No content, -blank, or -3mblank specified; nothing to write")
	}

	// Write AFI if requested
	if *afi != 0 {
		log.Printf("AFI %s with %d", sid, *afi)
		err := reader.WriteAfi(sid, byte(*afi))
		if err != nil {
			log.Fatalf("WriteAfi: %v", err)
		}
	}

	log.Printf("Done")
}
