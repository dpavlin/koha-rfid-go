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
	"koha-rfid/internal/rfidops"
)

// CLI tool that continuously scans RFID tags and prints enter/leave events.
//
// Usage:
//   scan -port /dev/ttyUSB0 [-debug] [-loop] [-log file.csv]

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

		result, err := rfidops.Scan(reader)
		if err != nil {
			log.Printf("Scan error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Build set of current tags
		current := make(map[string]bool)
		for _, t := range result.Tags {
			current[t.SID] = true
		}

		// Enter detection: print when tag was NOT in previous scan
		for _, info := range result.Tags {
			if !prevTags[info.SID] {
				fmt.Printf("%s reader 3M810 enter %s AFI: %s %s\n",
					isoDate(), strings.ToUpper(info.SID), info.Security, info.Content)

				// Log to CSV on first appearance
				if logFh != nil && info.Content != "" {
					fmt.Fprintf(logFh, "%s,%s,%s\n", isoDate(), strings.ToUpper(info.SID), info.Content)
				}
			}
			prevTags[info.SID] = true
		}

		// Leave detection: tags that were in prevTags but not in current
		for tag := range prevTags {
			if !current[tag] {
				fmt.Printf("%s leave %s\n", isoDate(), strings.ToUpper(tag))
				delete(prevTags, tag)
			}
		}

		// Print visible tags summary
		sids := make([]string, len(result.Tags))
		for i, info := range result.Tags {
			sids[i] = info.SID
		}
		if len(result.Tags) > 0 {
			fmt.Printf("%s visible: %s\n", isoDate(), strings.Join(sids, " "))
		} else {
			fmt.Printf("%s visible: \n", isoDate())
		}

		if !*loop {
			break
		}
		time.Sleep(1 * time.Second)
	}
}
