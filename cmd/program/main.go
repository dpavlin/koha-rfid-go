package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"koha-rfid/internal/rfid"
	"koha-rfid/internal/rfidops"
)

// CLI tool to program an RFID tag with content.
//
// Usage:
//   program -port /dev/ttyUSB0 [-debug] [-afi 214] [-type 1] [-set 1] [-total 1] E0_RFID_SID [barcode]
//   program -port /dev/ttyUSB0 -3mblank E0_RFID_SID
//
// The SID and barcode can also be comma-separated: program -port /dev/ttyUSB0 E0SID,barcode

func main() {
	comPort := flag.String("port", "/dev/ttyUSB0", "Serial port for 3M RFID reader")
	debug := flag.Bool("debug", false, "Enable debug logging")
	afi := flag.Int("afi", 0, "AFI byte value to write (e.g., 214 for secure, 218 for unsecure)")
	typeOpt := flag.Int("type", 0, "RFID501 item type (1=Book, 6=CD, etc.)")
	set := flag.Int("set", 1, "Set index (0-15)")
	total := flag.Int("total", 1, "Total items in set (0-15)")
	branch := flag.Int("branch", 0, "Branch number (12 bits, 0-4095)")
	library := flag.Int("library", 0, "Library number (20 bits, 0-1048575)")
	blank := flag.Bool("blank", false, "Write generic blank tag (3x zero blocks)")
	blank3M := flag.Bool("3mblank", false, "Write 3M manufacturing blank (6x 0x55 + zeros)")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s [options] E0_RFID_SID [barcode]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s [options] E0SID,barcode\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	sid := strings.ToUpper(strings.TrimSpace(args[0]))
	content := ""
	if len(args) >= 2 {
		content = args[1]
	}

	// Support comma-separated SID,content
	if strings.Contains(sid, ",") && content == "" {
		parts := strings.Split(sid, ",")
		if len(parts) >= 2 {
			sid = strings.ToUpper(strings.TrimSpace(parts[0]))
			content = parts[1]
		}
	}

	if len(sid) != 16 {
		log.Fatalf("SID must be 16 hex chars, got %q (len=%d)", sid, len(sid))
	}
	if len([]byte(content)) > 16 {
		log.Fatalf("barcode content must be at most 16 bytes for RFID501, got %d", len([]byte(content)))
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

	// Handle 3M blank separately (uses different block data)
	if *blank3M {
		log.Printf("BLANK 3mblank %s", sid)
		blocksHex := rfid.Blank3MRFID501()
		if err := reader.WriteBlocks(sid, blocksHex); err != nil {
			log.Fatalf("WriteBlocks 3mblank: %v", err)
		}
		if *afi != 0 {
			log.Printf("AFI %s with %d", sid, *afi)
			if err := reader.WriteAfi(sid, byte(*afi)); err != nil {
				log.Fatalf("WriteAfi: %v", err)
			}
		}
		log.Printf("Done")
		return
	}

	// Build ProgramOp
	var op rfidops.ProgramOp

	if *blank {
		log.Printf("BLANK blank %s", sid)
		op = rfidops.ProgramOp{SID: sid, Content: "blank"}
	} else if content != "" {
		log.Printf("PROGRAM %s with %s", sid, content)

		// Auto-detect type if not specified
		itemType := byte(*typeOpt)
		if itemType == 0 {
			if strings.HasPrefix(content, "130") {
				itemType = rfid.ItemTypeBook
			} else {
				itemType = rfid.ItemTypeOther
			}
		}

		op = rfidops.ProgramOp{
			SID:     sid,
			Content: content,
			RFID501Tag: &rfid.RFID501Tag{
				Set:     *set,
				Total:   *total,
				Type:    itemType,
				Content: content,
				Branch:  *branch,
				Library: *library,
				Custom:  0,
			},
		}
	} else {
		log.Fatalf("No content, -blank, or -3mblank specified; nothing to write")
	}

	res := rfidops.Program(reader, []rfidops.ProgramOp{op})
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			log.Printf("Program error: %s", e)
		}
		log.Fatalf("Program failed")
	}

	// Write AFI if requested (overrides Program's auto-detected AFI)
	if *afi != 0 {
		log.Printf("AFI %s with %d", sid, *afi)
		if err := reader.WriteAfi(sid, byte(*afi)); err != nil {
			log.Fatalf("WriteAfi: %v", err)
		}
	}

	log.Printf("Done")
}
