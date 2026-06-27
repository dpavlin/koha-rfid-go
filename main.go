package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	comPort := flag.String("com", "COM3", "Serial port for 3M RFID reader (e.g., COM3 on Windows)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	listen := flag.String("listen", "localhost:9000", "HTTP server listen address")
	onlyScan := flag.Bool("scan", false, "Scan once and exit (no HTTP server)")
	flag.Parse()

	// Open RFID reader
	log.Printf("Opening RFID reader on %s ...", *comPort)
	rfid, err := NewRfidReader(*comPort, *debug)
	if err != nil {
		log.Fatalf("RFID reader: %v", err)
	}
	defer rfid.Close()

	// Probe reader
	hwVer, err := rfid.Probe()
	if err != nil {
		log.Fatalf("RFID probe failed: %v", err)
	}
	log.Printf("3M 810 hardware version: %s", hwVer)

	if *onlyScan {
		tags, err := rfid.Inventory()
		if err != nil {
			log.Fatalf("Inventory scan failed: %v", err)
		}
		fmt.Printf("Tags in range: %d\n", len(tags))
		for _, t := range tags {
			fmt.Printf("  Tag: %s\n", t)
			afi, err := rfid.ReadAfi(t)
			if err == nil {
				fmt.Printf("    AFI: %02x\n", afi)
			}
			blocks, err := rfid.ReadBlocks(t, 0, 8)
			if err == nil {
				for bn, bp := range blocks {
					fmt.Printf("    Block %d: %s\n", bn, bp)
				}
			}
		}
		return
	}

	// Start HTTP server with only the RFID reader – no Koha API needed
	server := NewHttpServer(*listen, rfid, *debug)
	go func() {
		log.Printf("Starting HTTP server on %s", *listen)
		if err := server.Run(); err != nil {
			log.Fatalf("HTTP server: %v", err)
		}
	}()

	// Background scan loop that updates the tag cache
	go func() {
		for {
			tags, err := rfid.Inventory()
			if err != nil {
				log.Printf("Scan error: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}
			// Update tag cache with fresh data
			for _, tag := range tags {
				info := &TagInfo{
					SID:     strings.ToUpper(tag),
					TagType: "RFID501",
					Reader:  "3M810",
				}
				afi, err := rfid.ReadAfi(tag)
				if err == nil {
					info.Security = strings.ToUpper(hex.EncodeToString([]byte{afi}))
				}
				blocks, err := rfid.ReadBlocks(tag, 0, 8)
				if err == nil {
					if b0, ok := blocks[0]; ok {
						barcodeBytes, _ := hex.DecodeString(b0)
						info.Content = string(barcodeBytes)
					}
				}
				server.tagCache[info.SID] = info
			}
			// Remove stale tags
			for sid := range server.tagCache {
				found := false
				for _, t := range tags {
					if strings.EqualFold(t, sid) {
						found = true
						break
					}
				}
				if !found {
					delete(server.tagCache, sid)
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down...")
}
