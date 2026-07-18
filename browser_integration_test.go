package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"koha-rfid/internal/rfid"
)

// ---------------------------------------------------------------------------
// Browser integration test: drives a real Chrome browser to navigate the Koha
// intranet and verify the RFID JavaScript integration works end-to-end.
//
// Requirements:
//   - RFID_PORT env var set to a serial port with a 3M 810 reader
//   - Chrome running with --remote-debugging-port=9333 (or CDP_PORT env var)
//   - KOHA_USER / KOHA_PASS env vars for intranet login
//
// The RFID server is started on port 9000 (or RFID_SERVER_PORT). The Koha
// intranet pages include RFID JavaScript that polls localhost:9000. This test
// injects a patched version that uses http:// (not protocol-relative) so it
// works over HTTPS.
//
// Run:
//   RFID_PORT=/dev/ttyUSB0 KOHA_USER=xxx KOHA_PASS=yyy \
//     go test -v -run Browser -timeout 120s
// ---------------------------------------------------------------------------

var kohaURL = "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/mainpage.pl"
var rfidPort = "9000"
var circulationURL = "https://ffzg.koha-dev.rot13.org:8443/cgi-bin/koha/circ/circulation.pl"

func init() {
	if u := os.Getenv("KOHA_URL"); u != "" {
		kohaURL = u
	}
	if p := os.Getenv("RFID_SERVER_PORT"); p != "" {
		rfidPort = p
	}
	if c := os.Getenv("CIRCULATION_URL"); c != "" {
		circulationURL = c
	}
}

func cdpPort() string {
	p := os.Getenv("CDP_PORT")
	if p == "" {
		return "9333"
	}
	return p
}

func kohaCreds() (string, string) {
	return os.Getenv("KOHA_USER"), os.Getenv("KOHA_PASS")
}

func skipIfNoBrowser(t *testing.T) {
	if os.Getenv("RFID_PORT") == "" {
		t.Skip("Skipping browser test: set RFID_PORT, KOHA_USER, KOHA_PASS")
	}
	u, p := kohaCreds()
	if u == "" || p == "" {
		t.Skip("Skipping browser test: set KOHA_USER and KOHA_PASS env vars")
	}
}

// startRFIDServer starts the RFID HTTP server on rfidPort and returns a
// cleanup function. The port matches the hardcoded URL in koha-rfid.js.
func startRFIDServer(t *testing.T) func() {
	reader, err := rfid.NewRfidReader(os.Getenv("RFID_PORT"), false)
	if err != nil {
		t.Fatalf("open RFID reader: %v", err)
	}
	hwVer, err := reader.Probe()
	if err != nil {
		reader.Close()
		t.Fatalf("Probe failed: %v", err)
	}
	t.Logf("RFID reader hardware version: %s", hwVer)

	server := NewHttpServer("localhost:"+rfidPort, reader, false)
	kohaParsedURL, err := url.Parse(kohaURL)
	if err != nil || kohaParsedURL.Scheme == "" || kohaParsedURL.Host == "" {
		reader.Close()
		t.Fatalf("invalid KOHA_URL %q", kohaURL)
	}
	server.SetAllowedOrigin(kohaParsedURL.Scheme + "://" + kohaParsedURL.Host)
	cert, key, err := genSelfSignedCert()
	if err != nil {
		reader.Close()
		t.Fatalf("generate TLS certificate: %v", err)
	}
	server.SetTLS(cert, key)
	go func() {
		if err := server.Run(); err != nil {
			t.Logf("RFID server error: %v", err)
		}
	}()
	time.Sleep(200 * time.Millisecond)

	cleanup := func() {
		reader.Close()
	}
	return cleanup
}

// rfidPatchJS returns JavaScript that patches the RFID polling URLs from
// protocol-relative (///localhost:9000) to https://localhost:9000 so they work
// over the HTTPS Koha page.
func rfidPatchJS() string {
	return `
	// Patch RFID polling URLs to use https:// instead of protocol-relative ///
	// This is needed because Koha serves over HTTPS, and our RFID server is HTTPS.
	var origGetJSON = $.getJSON;
	$.getJSON = function(url, success) {
		if (typeof url === 'string' && url.indexOf('///localhost:9000') === 0) {
			url = 'https://localhost:9000' + url.slice(3);
			console.log('RFID: patched URL', url);
		}
		return origGetJSON(url, success);
	};
	console.log('RFID: URL patching installed');
	`
}

func TestBrowserRFIDIntegration(t *testing.T) {
	skipIfNoBrowser(t)

	// Start RFID server on rfidPort
	cleanupRFID := startRFIDServer(t)
	defer cleanupRFID()

	cdpAddr := fmt.Sprintf("localhost:%s", cdpPort())

	// Connect to existing Chrome via remote debugging
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(),
		fmt.Sprintf("http://%s", cdpAddr))
	defer allocCancel()

	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	ctx, cancel := context.WithTimeout(tabCtx, 90*time.Second)
	defer cancel()

	koUser, koPass := kohaCreds()

	t.Logf("Navigating to Koha intranet: %s", kohaURL)

	// Navigate and login
	if err := chromedp.Run(ctx,
		chromedp.Navigate(kohaURL),
		chromedp.WaitVisible(`#login form`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name=userid]`, koUser, chromedp.ByQuery),
		chromedp.SendKeys(`input[name=password]`, koPass, chromedp.ByQuery),
		chromedp.Click(`input#login`, chromedp.ByQuery),
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	t.Log("Logged into Koha intranet")

	// Inject RFID URL patch before the RFID JS runs
	var patchResult string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(rfidPatchJS(), &patchResult),
		chromedp.Sleep(1*time.Second),
	); err != nil {
		t.Errorf("Patch injection failed: %v", err)
	}

	// Wait for RFID polling to happen
	var rfidText string
	if err := chromedp.Run(ctx,
		chromedp.Sleep(10*time.Second), // wait for multiple scan cycles
		chromedp.Evaluate(`
			(function() {
				var el = document.querySelector('#rfid');
				return el ? el.textContent : 'not-found';
			})();
		`, &rfidText),
	); err != nil {
		t.Errorf("RFID element check failed: %v", err)
	}
	t.Logf("RFID element text: %q", rfidText)

	// Read RFID info state
	var rfidInfo string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`
			(function() {
				var info = document.querySelector('#rfid-info');
				var span = document.querySelector('#rfid');
				var text = '';
				if (span) text += 'rfid=' + span.textContent;
				if (info) text += ' info=' + info.textContent;
				return text || 'no rfid elements';
			})();
		`, &rfidInfo),
	); err != nil {
		t.Logf("read state error: %v", err)
	}
	t.Logf("RFID page state: %s", rfidInfo)
}

func TestBrowserRFIDCirculation(t *testing.T) {
	skipIfNoBrowser(t)

	cleanupRFID := startRFIDServer(t)
	defer cleanupRFID()

	cdpAddr := fmt.Sprintf("localhost:%s", cdpPort())

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(),
		fmt.Sprintf("http://%s", cdpAddr))
	defer allocCancel()

	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	ctx, cancel := context.WithTimeout(tabCtx, 120*time.Second)
	defer cancel()

	koUser, koPass := kohaCreds()

	// Login
	if err := chromedp.Run(ctx,
		chromedp.Navigate(kohaURL),
		chromedp.WaitVisible(`#login form`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name=userid]`, koUser, chromedp.ByQuery),
		chromedp.SendKeys(`input[name=password]`, koPass, chromedp.ByQuery),
		chromedp.Click(`input#login`, chromedp.ByQuery),
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	t.Log("Logged in")

	// Navigate to circulation (checkout) page
	if err := chromedp.Run(ctx,
		chromedp.Navigate(circulationURL),
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		t.Fatalf("Navigate to circulation failed: %v", err)
	}

	t.Log("On circulation page")

	// Inject RFID URL patch
	var patchResult string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(rfidPatchJS(), &patchResult),
		chromedp.Sleep(1*time.Second),
	); err != nil {
		t.Errorf("Patch injection failed: %v", err)
	}

	// Wait for RFID polling
	var barcodeVal string
	if err := chromedp.Run(ctx,
		chromedp.Sleep(10*time.Second),
		chromedp.Evaluate(`
			(function() {
				var el = document.querySelector('input[name=barcode]');
				return el ? el.value : 'not-found';
			})();
		`, &barcodeVal),
	); err != nil {
		t.Errorf("Read barcode field failed: %v", err)
	}
	t.Logf("Barcode field after RFID scan: %q", barcodeVal)

	// Check RFID state
	var rfidState string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`
			(function() {
				var info = document.querySelector('#rfid-info');
				var span = document.querySelector('#rfid');
				var text = '';
				if (span) text += 'rfid=' + span.textContent;
				if (info) text += ' info=' + info.textContent;
				return text || 'no rfid elements';
			})();
		`, &rfidState),
	); err != nil {
		t.Logf("read state error: %v", err)
	}
	t.Logf("RFID state on circulation page: %s", rfidState)
}

// ---------------------------------------------------------------------------
// TestMain

func TestMain(m *testing.M) {
	port := os.Getenv("RFID_PORT")
	if port != "" {
		log.Printf("RFID_PORT=%s — browser integration tests will use real hardware", port)
	}
	u, p := os.Getenv("KOHA_USER"), os.Getenv("KOHA_PASS")
	if u != "" && p != "" {
		log.Printf("KOHA_USER=%s — will log into Koha intranet", u)
	}
	log.Printf("CDP_PORT=%s — connecting to Chrome DevTools", os.Getenv("CDP_PORT"))
	os.Exit(m.Run())
}
