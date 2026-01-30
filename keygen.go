package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Blue   = "\033[34m"
	Pink   = "\033[35m"
	Purple = "\033[35m"
	Red    = "\033[31m"
	apiURL = "https://api.quantumnumbers.anu.edu.au?length=100&type=hex8&size=1"
)

// Shared client with connection pooling to avoid TCP handshake overhead on every call
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

func init() {
	if runtime.GOOS == "windows" {
		var handle = syscall.Handle(os.Stdout.Fd())
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		var mode uint32
		kernel32.NewProc("GetConsoleMode").Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
		mode |= 0x0004
		kernel32.NewProc("SetConsoleMode").Call(uintptr(handle), uintptr(mode))
	}
}

type apiResponse struct {
	Success bool     `json:"success"`
	Data    []string `json:"data"`
	Message string   `json:"error_msg"`
}

// just the help menu
func printHelp() {
	divider := strings.Repeat("-", 55)
	fmt.Println(divider)
	fmt.Println(Bold + Purple + " Quantum Key Generator (Optimized)" + Reset)
	fmt.Println(divider)
	fmt.Println(Pink + " OVERVIEW:" + Reset)
	fmt.Println("  Generates keys using ANU Quantum Vacuum entropy.")
	fmt.Println("  Uses rejection sampling and optimized hex decoding.")
	fmt.Println()
	fmt.Println(Pink + " FLAGS:" + Reset)
	flag.PrintDefaults()
	fmt.Println(divider)
}

// Main worker function
func worker(apiKey string, entropyChan chan<- byte, stopChan <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-stopChan:
			return
		default:
			req, _ := http.NewRequest("GET", apiURL, nil)
			req.Header.Set("x-api-key", apiKey)

			resp, err := httpClient.Do(req)
			if err != nil {
				continue
			}

			var parsed apiResponse
			// Optimized: Stream directly from the response body to the JSON decoder
			err = json.NewDecoder(resp.Body).Decode(&parsed)
			resp.Body.Close()

			if err == nil && parsed.Success {
				for _, hexStr := range parsed.Data {
					// Optimized: hex.DecodeString is significantly faster than fmt.Sscanf
					if b, err := hex.DecodeString(hexStr); err == nil && len(b) > 0 {
						select {
						case entropyChan <- b[0]:
						case <-stopChan:
							return
						}
					}
				}
			}
		}
	}
}

// generate key 
func generate(apiKey string, length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{};:,.<>?"
	limit := 256 - (256 % len(charset))

	// buffer the channel to prevent network workers from blocking
	entropyChan := make(chan byte, length*2)
	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// scale workers based on CPU count to handle network I/O efficiently
	numWorkers := runtime.NumCPU() * 2
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(apiKey, entropyChan, stopChan, &wg)
	}

	var pwd strings.Builder
	pwd.Grow(length) // pre-allocate memory to avoid multiple re-allocations

	for pwd.Len() < length {
		b := <-entropyChan
		if int(b) < limit {
			pwd.WriteByte(charset[int(b)%len(charset)])
		}
	}

	close(stopChan)
	wg.Wait()
	return pwd.String(), nil
}

func main() {
	key := flag.String("key", "", "Your ANU Quantum API Key")
	lenFlag := flag.Int("len", 30, "Length of the generated string")
	outFlag := flag.String("out", "", "Output result to a text file")

	flag.Usage = printHelp
	flag.Parse()

	if *key == "" {
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("%s[INIT]%s Accessing quantum stream...\n", Purple, Reset)
	start := time.Now()
	result, err := generate(*key, *lenFlag)
	if err != nil {
		fmt.Printf("%s[ERROR] %v%s\n", Red, err, Reset)
		return
	}

	if *outFlag != "" {
		_ = os.WriteFile(*outFlag, []byte(result), 0600)
		fmt.Printf("%s[+] Saved to %s%s\n", Pink, *outFlag, Reset)
	}

	div := strings.Repeat("-", 55)
	fmt.Println(div)
	fmt.Printf("%s[+] RESULT:%s\n", Pink, Reset)
	fmt.Printf("%s%s%s%s\n", Bold, Blue, result, Reset)
	fmt.Println(div)
	fmt.Printf("%s[i] GEN_TIME:%s %v\n", Pink, Reset, time.Since(start))
}
