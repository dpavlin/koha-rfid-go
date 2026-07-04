package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"koha-rfid/internal/rfid"
)

// HttpServer provides the local JSONP API for the Koha JavaScript integration.
type HttpServer struct {
	listen   string
	mu       sync.Mutex
	rfid     *rfid.RfidReader
	debug    bool
	tagCache map[string]*TagInfo
}

type TagInfo struct {
	SID      string `json:"sid"`
	Content  string `json:"content"`
	Security string `json:"security"`
	TagType  string `json:"tag_type"`
	Reader   string `json:"reader"`
}

func NewHttpServer(listen string, reader *rfid.RfidReader, debug bool) *HttpServer {
	return &HttpServer{
		listen:   listen,
		rfid:     reader,
		debug:    debug,
		tagCache: make(map[string]*TagInfo),
	}
}

func (s *HttpServer) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/scan/", s.handleScan)
	mux.HandleFunc("/scan/only/", s.handleScan)
	mux.HandleFunc("/secure", s.handleSecure)
	mux.HandleFunc("/secure.js", s.handleSecureJSONP)
	mux.HandleFunc("/program", s.handleProgram)

	// Static file serving for the JavaScript example
	mux.Handle("/examples/", http.StripPrefix("/examples/", http.FileServer(http.Dir("examples"))))

	addr := s.listen
	if addr == "" {
		addr = "localhost:9000"
	}
	log.Printf("HTTP server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *HttpServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html><html><head><title>RFID Server</title></head><body>
<h1>RFID Server</h1><p>Status: OK</p></body></html>`
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(200)
	w.Write([]byte(html))
}

func (s *HttpServer) handleScan(w http.ResponseWriter, r *http.Request) {
	callback := r.FormValue("callback")

	s.mu.Lock()
	tags, err := s.rfid.Inventory()
	if err != nil {
		s.mu.Unlock()
		http.Error(w, fmt.Sprintf("RFID error: %v", err), 500)
		return
	}

	result := map[string]interface{}{
		"time": time.Now().Unix(),
	}

	var tagList []map[string]interface{}
	for _, tag := range tags {
		info := &TagInfo{
			SID:     strings.ToUpper(tag),
			TagType: "RFID501",
			Reader:  "3M810",
		}

		// Read AFI
		afi, err := s.rfid.ReadAfi(tag)
		if err == nil {
			info.Security = strings.ToUpper(hex.EncodeToString([]byte{afi}))
		}

		// Read blocks and decode RFID501
		blocks, err := s.rfid.ReadBlocks(tag, 0, 8)
		if err == nil && len(blocks) > 0 {
			blockHexes := make([]string, len(blocks))
			for i := 0; i < len(blocks); i++ {
				if b, ok := blocks[i]; ok {
					blockHexes[i] = b
				}
			}
			decoded := rfid.DecodeRFID501(blockHexes)
			if decoded != nil {
				info.Content = decoded.Content
				info.TagType = "RFID501"
			} else {
				// Fallback: read block 0 as raw barcode
				if b0, ok := blocks[0]; ok {
					barcodeBytes, _ := hex.DecodeString(b0)
					info.Content = string(barcodeBytes)
				}
			}
		}

		// Cache the tag info
		s.tagCache[info.SID] = info

		item := map[string]interface{}{
			"sid":      info.SID,
			"content":  info.Content,
			"security": info.Security,
			"tag_type": info.TagType,
			"reader":   info.Reader,
		}
		tagList = append(tagList, item)
	}
	s.mu.Unlock()

	result["tags"] = tagList
	jsonBytes, _ := json.Marshal(result)

	if callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "%s(%s)", callback, jsonBytes)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
	}
}

func (s *HttpServer) handleSecure(w http.ResponseWriter, r *http.Request) {
	status := 302
	r.ParseForm()
	for key, vals := range r.Form {
		if len(key) == 16 {
			tag := strings.ToUpper(key)
			afiHex := vals[0]
			afiByte, err := hex.DecodeString(afiHex)
			if err != nil || len(afiByte) != 1 {
				http.Error(w, "invalid AFI", 400)
				return
			}
			if s.debug {
				log.Printf("SECURE %s -> AFI %s", tag, afiHex)
			}
			s.mu.Lock()
			err = s.rfid.WriteAfi(tag, afiByte[0])
			s.mu.Unlock()
			if err != nil {
				log.Printf("SECURE error: %v", err)
				http.Error(w, err.Error(), 500)
				return
			}
			status = 200
		}
	}
	w.Header().Set("Location", fmt.Sprintf("http://%s/", s.listen))
	w.WriteHeader(status)
}

func (s *HttpServer) handleSecureJSONP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	callback := r.FormValue("callback")
	for key, vals := range r.Form {
		if len(key) != 16 || strings.HasPrefix(key, "call") || strings.HasPrefix(key, "_") {
			continue
		}
		tag := strings.ToUpper(key)
		afiHex := vals[0]
		afiByte, err := hex.DecodeString(afiHex)
		if err != nil || len(afiByte) != 1 {
			errResp := fmt.Sprintf(`{"ok":0,"error":"invalid AFI hex %s"}`, afiHex)
			if callback != "" {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprintf(w, "%s(%s)", callback, errResp)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(errResp))
			}
			return
		}
		s.mu.Lock()
		err = s.rfid.WriteAfi(tag, afiByte[0])
		s.mu.Unlock()
		if err != nil {
			errResp := fmt.Sprintf(`{"ok":0,"error":"%v"}`, err)
			log.Printf("SECURE error: %v", err)
			if callback != "" {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprintf(w, "%s(%s)", callback, errResp)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(errResp))
			}
			return
		}
	}
	jsonResp := `{"ok":1}`
	if callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "%s(%s)", callback, jsonResp)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jsonResp))
	}
}

func (s *HttpServer) handleProgram(w http.ResponseWriter, r *http.Request) {
	// Program a tag: /program?<tag>=<content>&<tag>=<content>
	// Content can be a barcode string (e.g., "1301234567") or "blank" for a blank tag.
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form parse error", 400)
		return
	}
	for key, vals := range r.Form {
		if len(key) != 16 || strings.HasPrefix(key, "call") || strings.HasPrefix(key, "_") {
			continue
		}
		tag := strings.ToUpper(key)
		content := vals[0]

		// Blank tag: write 3 blocks of zeros
		if strings.ToLower(content) == "blank" {
			s.mu.Lock()
			blocksHex := rfid.BlankRFID501()
			err := s.rfid.WriteBlocks(tag, blocksHex)
			if err != nil {
				s.mu.Unlock()
				log.Printf("PROGRAM blank error: %v", err)
				http.Error(w, err.Error(), 500)
				return
			}
			// AFI unsecure for blank tags
			err = s.rfid.WriteAfi(tag, rfid.AfiUnsecure)
			s.mu.Unlock()
			if err != nil {
				log.Printf("PROGRAM AFI error: %v", err)
			}
			continue
		}

		// Encode content as RFID501 format (8 blocks)
		s.mu.Lock()
		rfid501Hex := rfid.EncodeRFID501Content(content)

		// Write all 8 blocks in one command
		err := s.rfid.WriteBlocks(tag, rfid501Hex)
		if err != nil {
			s.mu.Unlock()
			log.Printf("PROGRAM error: %v", err)
			http.Error(w, err.Error(), 500)
			return
		}

		// Set AFI based on content: books (130 prefix) get secure, others unsecure
		afi := rfid.AfiUnsecure
		if strings.HasPrefix(content, "130") {
			afi = rfid.AfiSecure
		}
		err = s.rfid.WriteAfi(tag, afi)
		s.mu.Unlock()
		if err != nil {
			log.Printf("PROGRAM AFI error: %v", err)
		}
	}

	callback := r.FormValue("callback")
	if callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "%s({ok:1})", callback)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":1}`))
	}
}
