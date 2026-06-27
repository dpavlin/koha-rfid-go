package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// KohaClient handles communication with Koha ILS
type KohaClient struct {
	BaseURL   string // Koha base URL (e.g., http://koha.example.com:8080)
	SipServer string // SIP2 server address (e.g., "10.0.0.1:6002")
	SipUser   string
	SipPass   string
	SipLoc    string // SIP2 location code
	debug     bool
}

func NewKohaClient(baseURL string, debug bool) *KohaClient {
	return &KohaClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		debug:   debug,
	}
}

// SetSipConfig sets SIP2 connection parameters
func (k *KohaClient) SetSipConfig(server, user, pass, loc string) {
	k.SipServer = server
	k.SipUser = user
	k.SipPass = pass
	k.SipLoc = loc
}

// CheckoutViaRest performs a checkout via Koha REST API.
// Requires patron barcode and item barcode.
func (k *KohaClient) CheckoutViaRest(patronBarcode, itemBarcode string) (bool, string, error) {
	apiURL := k.BaseURL + "/api/v1/checkout/"
	body := map[string]string{
		"patron_barcode": patronBarcode,
		"item_barcode":   itemBarcode,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(apiURL, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return false, "", fmt.Errorf("checkout REST request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		var result map[string]interface{}
		json.Unmarshal(respBody, &result)
		msg, _ := result["message"].(string)
		return true, msg, nil
	}
	return false, string(respBody), fmt.Errorf("checkout failed: %d %s", resp.StatusCode, respBody)
}

// CheckinViaRest performs a check-in via Koha REST API.
func (k *KohaClient) CheckinViaRest(itemBarcode string) (bool, string, error) {
	apiURL := k.BaseURL + "/api/v1/checkin/"
	body := map[string]string{
		"item_barcode": itemBarcode,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(apiURL, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return false, "", fmt.Errorf("checkin REST request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		return true, string(respBody), nil
	}
	return false, string(respBody), fmt.Errorf("checkin failed: %d %s", resp.StatusCode, respBody)
}

// Sip2Login logs into the SIP2 server
func (k *KohaClient) Sip2Login() (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", k.SipServer, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("SIP2 dial: %w", err)
	}
	loginMsg := fmt.Sprintf("9300CN%s|CO%s|", k.SipUser, k.SipPass)
	resp, err := sip2Message(conn, loginMsg, k.debug)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if len(resp.Fixed) < 3 || resp.Fixed[:3] != "941" {
		conn.Close()
		return nil, fmt.Errorf("SIP2 login failed: %s", resp.Fixed)
	}
	return conn, nil
}

type Sip2Response struct {
	Fixed  string
	Fields map[string]string
}

func sip2Message(conn net.Conn, msg string, debug bool) (*Sip2Response, error) {
	if !strings.HasSuffix(msg, "\r\n") {
		msg = strings.TrimRight(msg, "\r\n") + "\r\n"
	}
	if debug {
		log.Printf("SIP2 >> %q", msg)
	}
	fmt.Fprint(conn, msg)

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("SIP2 read: %w", err)
	}
	reply := string(buf[:n])
	reply = strings.TrimRight(reply, "\r\n")
	if debug {
		log.Printf("SIP2 << %q", reply)
	}

	var resp Sip2Response
	resp.Fields = make(map[string]string)

	fields := strings.Split(reply, "|")
	if len(fields) > 0 {
		resp.Fixed = fields[0]
	}
	for _, f := range fields[1:] {
		if len(f) >= 2 {
			code := f[:2]
			val := f[2:]
			resp.Fields[code] = val
		}
	}
	return &resp, nil
}

// Sip2Checkout performs SIP2 checkout
func (k *KohaClient) Sip2Checkout(patronBarcode, itemBarcode string, tagSid string) (bool, string, error) {
	conn, err := k.Sip2Login()
	if err != nil {
		return false, "", err
	}
	defer conn.Close()

	now := time.Now()
	ts := now.Format("20060102") + "    " + now.Format("150405")

	msg := fmt.Sprintf("11YN%s                  AO%s|AA%s|AB%s|AC%s|BON|BIN|",
		ts, k.SipLoc, patronBarcode, itemBarcode, k.SipPass)
	resp, err := sip2Message(conn, msg, k.debug)
	if err != nil {
		return false, "", err
	}
	if len(resp.Fixed) >= 3 && resp.Fixed[2] == '1' {
		return true, "", nil
	}
	return false, resp.Fixed, fmt.Errorf("SIP2 checkout rejected: %s", resp.Fixed)
}

// Sip2Checkin performs SIP2 check-in
func (k *KohaClient) Sip2Checkin(itemBarcode string) (bool, string, error) {
	conn, err := k.Sip2Login()
	if err != nil {
		return false, "", err
	}
	defer conn.Close()

	now := time.Now()
	ts := now.Format("20060102") + "    " + now.Format("150405")

	msg := fmt.Sprintf("09N%s%sAP|AO%s|AB%s|AC|BIN|", ts, ts, k.SipLoc, itemBarcode)
	resp, err := sip2Message(conn, msg, k.debug)
	if err != nil {
		return false, "", err
	}
	if len(resp.Fixed) >= 3 && resp.Fixed[2] == '1' {
		return true, "", nil
	}
	return false, resp.Fixed, fmt.Errorf("SIP2 checkin rejected: %s", resp.Fixed)
}

// GetPatronBySid fetches patron info from Koha by RFID SID (for SmartX tags)
func (k *KohaClient) GetPatronBySid(sid string) (map[string]interface{}, error) {
	apiURL, _ := url.Parse(k.BaseURL + "/cgi-bin/koha/ffzg/rfid/borrower.pl")
	params := url.Values{}
	params.Add("RFID_SID", sid)
	params.Add("OIB", "")
	params.Add("JMBAG", "")
	apiURL.RawQuery = params.Encode()

	resp, err := http.Get(apiURL.String())
	if err != nil {
		return nil, fmt.Errorf("borrower request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result, nil
}
