package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"koha-rfid/internal/rfid"
	"koha-rfid/internal/rfidops"
)

func main() {
	comPort := flag.String("port", "/dev/ttyUSB0", "Serial port for 3M RFID reader")
	debug := flag.Bool("debug", false, "Enable debug logging")
	listen := flag.String("listen", "localhost:9000", "HTTP server listen address")
	onlyScan := flag.Bool("scan", false, "Scan once and exit (no HTTP server)")
	flag.Parse()

	// Open RFID reader
	log.Printf("Opening RFID reader on %s ...", *comPort)
	reader, err := rfid.NewRfidReader(*comPort, *debug)
	if err != nil {
		log.Fatalf("RFID reader: %v", err)
	}
	defer reader.Close()

	// Probe reader
	hwVer, err := reader.Probe()
	if err != nil {
		log.Fatalf("RFID probe failed: %v", err)
	}
	log.Printf("3M 810 hardware version: %s", hwVer)

	if *onlyScan {
		result, err := rfidops.Scan(reader)
		if err != nil {
			log.Fatalf("Inventory scan failed: %v", err)
		}
		fmt.Printf("Tags in range: %d\n", len(result.Tags))
		for _, t := range result.Tags {
			fmt.Printf("  Tag: %s\n", t.SID)
			fmt.Printf("    Security: %s\n", t.Security)
			fmt.Printf("    Content: %s\n", t.Content)
		}
		return
	}

	// Start HTTP server
	server := NewHttpServer(*listen, reader, *debug)
	go func() {
		log.Printf("Starting HTTP server on %s", *listen)
		if err := server.Run(); err != nil {
			log.Fatalf("HTTP server: %v", err)
		}
	}()

	// Background scan loop that updates the tag cache
	go func() {
		for {
			if err := server.BackgroundScan(); err != nil {
				log.Printf("Scan error: %v", err)
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
