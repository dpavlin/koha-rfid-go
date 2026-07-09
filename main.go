package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"koha-rfid/internal/rfid"
	"koha-rfid/internal/rfidops"
)

func genSelfSignedCert() (certFile, keyFile string, err error) {
	certFile = "rfid-localhost.crt"
	keyFile = "rfid-localhost.key"

	// Reuse existing cert/key if they exist
	if _, errStat := os.Stat(certFile); errStat == nil {
		if _, errStat2 := os.Stat(keyFile); errStat2 == nil {
			log.Printf("Reusing existing cert: %s, key: %s", certFile, keyFile)
			return certFile, keyFile, nil
		}
	}

	// Generate a self-signed cert for localhost
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	// Use 10 year validity — browsers accept self-signed certs much longer than
	// the 398-day limit which only applies to publicly-trusted CAs.
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"RFID Server"}},
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("create cert: %w", err)
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		return "", "", err
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	certOut.Close()

	keyOut, err := os.Create(keyFile)
	if err != nil {
		return "", "", err
	}
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	keyOut.Close()

	log.Printf("Generated self-signed cert: %s, key: %s", certFile, keyFile)
	return certFile, keyFile, nil
}

func main() {
	comPort := flag.String("port", "/dev/ttyUSB0", "Serial port for 3M RFID reader")
	debug := flag.Bool("debug", false, "Enable debug logging")
	mock := flag.Bool("mock", false, "Use mock RFID reader for browser testing (no real reader needed)")
	listen := flag.String("listen", "localhost:9000", "HTTP server listen address")
	onlyScan := flag.Bool("scan", false, "Scan once and exit (no HTTP server)")
	tlsMode := flag.Bool("tls", false, "Serve HTTPS with auto-generated self-signed cert")
	flag.Parse()

	// Determine RFID ops: mock or real reader
	var ops rfidops.RfidOps

	if *mock {
		log.Println("Using mock RFID reader (no real hardware required)")
		ops = newMockOps()
	} else {
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
		ops = reader
	}

	if *onlyScan {
		result, err := rfidops.Scan(ops)
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
	server := NewHttpServer(*listen, ops, *debug)

	if *tlsMode {
		cert, key, err := genSelfSignedCert()
		if err != nil {
			log.Fatalf("TLS cert: %v", err)
		}
		server.SetTLS(cert, key)
	}
	go func() {
		log.Printf("Starting HTTP server on %s", *listen)
		if err := server.Run(); err != nil {
			log.Fatalf("HTTP server: %v", err)
		}
	}()

	// No background scan — RFID reader is polled only on HTTP request.

	// Wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down...")
}
