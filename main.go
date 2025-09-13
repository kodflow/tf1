// TEST 1: Est-ce que ça run/build ?
// go run main.go services.txt
// Opening services.txt
// Url: https://www.google.com; Status: 200; Latency: 82ms
// Url: https://www.github.com; Status: 200; Latency: 135ms
// Url: https://www.stackoverflow.com; Status: 200; Latency: 443ms
// OK

// TEST 2: Fichier manquant
// go run main.go
// missing file argument
// Une erreur avec un exit code 1 comportement standard

// Test 3: Fichier inexistant
// go run main.go doesnotexist.txt
// open doesnotexist.txt: no such file or directory
// Une erreur avec un exit code 1 comportement standard, mais pourrait être amélioré avec une code erreur différent > 1

// TEST 4: On lance les tests
// go test -v
// === RUN   TestHealthCheck
//--- FAIL: TestHealthCheck (0.00s)
//panic: TODO implements me [recovered, repanicked]
//
// goroutine 22 [running]:
// testing.tRunner.func1.2({0x1029bd0a0, 0x102a0a0b0})
//        /opt/homebrew/Cellar/go/1.25.1/libexec/src/testing/testing.go:1872 +0x190
// testing.tRunner.func1()
//        /opt/homebrew/Cellar/go/1.25.1/libexec/src/testing/testing.go:1875 +0x31c
// panic({0x1029bd0a0?, 0x102a0a0b0?})
//        /opt/homebrew/Cellar/go/1.25.1/libexec/src/runtime/panic.go:783 +0x120
// coding-challenge.TestHealthCheck(0x14000082e00?)
//        /Users/florent/Documents/tf1/main_test.go:18 +0x2c
// testing.tRunner(0x14000082e00, 0x102a09168)
//        /opt/homebrew/Cellar/go/1.25.1/libexec/src/testing/testing.go:1934 +0xc8
// created by testing.(*T).Run in goroutine 1
//        /opt/homebrew/Cellar/go/1.25.1/libexec/src/testing/testing.go:1997 +0x364
// exit status 2
// FAIL    coding-challenge        0.220s
// Un panic "TODO implements me" pour indiquer que le test n'est pas encore implémenté
// (opt1)soit j'implémente le test façon TDD avec les cas de test que je visualise
// (opt2)soit je continue la review du code et je reviens dessus après.

// TEST 5: Analyse statique du code, j'utilise toute une panoplie d'outils, golangci, codacy, sonar le plus strict possible
// golangci-lint run
// 8 issues:
// * errcheck: 1
// * gocritic: 1
// * gosec: 2
// * govet: 1
// * revive: 3
// et un WARNING: WARN [linters_context] copyloopvar: this linter is disabled because the Go version (1.18) of your project is lower than Go 1.22

// ACTION 1: intervention MAJ de la version de go dans le go.mod
// go mod edit -go=1.25
// go mod tidy

// TEST 6: golangci-lint run
// On fixe les erreurs
// OK (on fait comme si on n'avait pas vu l'erreur de race pour l'instant)

// ACTION 2: On va s'occuper du TODO et creer les tests unitaires facon TDD
// go test -race
// ===================
//	WARNING: DATA RACE

// ACTION 3: on défini des const en amont du projet
// ACTION 4: improve léger on ajoute un semaphore pour limiter la concurrence

// main.go:90:1: package-comments: should have a package comment (revive)
// Package main provides a health check utility for web services.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Configuration constants
const (
	// Concurrency settings
	MaxConcurrentRequests = 64 // Maximum number of concurrent HTTP requests

	// HTTP client settings
	MaxIdleConns        = 256              // Maximum idle connections across all hosts
	MaxIdleConnsPerHost = 64               // Maximum idle connections per host
	IdleConnTimeout     = 90 * time.Second // Idle connection timeout
	HTTPClientTimeout   = 30 * time.Second // Overall HTTP client timeout
	RequestTimeout      = 5 * time.Second  // Individual request timeout

	// Application settings
	UserAgent         = "tf1-healthcheck/1.0"
	HTTPScheme        = "http://"
	HTTPSScheme       = "https://"
	MinHTTPURLLength  = 7 // Minimum length for "http://"
	MinHTTPSURLLength = 8 // Minimum length for "https://"

	// Exit codes
	ExitSuccess = 0
	ExitError   = 1
)

// Mockable for testing
var (
	osGeteuid = os.Geteuid // Mock point for os.Geteuid
	osGetuid  = os.Getuid  // Mock point for os.Getuid
	osGetenv  = os.Getenv  // Mock point for os.Getenv
	osExit    = os.Exit    // Mock point for os.Exit
)

type Result struct {
	URL     string
	Status  int
	Err     error
	Latency time.Duration
}

// Package-level shared HTTP client with optimized transport settings
var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        MaxIdleConns,
		MaxIdleConnsPerHost: MaxIdleConnsPerHost,
		IdleConnTimeout:     IdleConnTimeout,
	},
	Timeout: HTTPClientTimeout,
}

func main() {
	osExit(run(os.Args))
}

func run(args []string) int {
	validateExecution()

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "missing file argument")
		return ExitError
	}

	path := args[1]
	fmt.Printf("Opening %s\n", path)

	// on assume the input file is not sensitive
	//nolint:gosec
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ExitError
	}

	//nolint:errcheck
	defer f.Close()

	// Always use the optimized streaming version for better performance and scalability
	return streamHealthCheck(f, os.Stdout)
}

// streamHealthCheck processes URLs in a streaming fashion for scalability
// Why this is better for millions of URLs:
// 1. Constant memory usage (O(1)) instead of O(n) - only MaxConcurrentRequests URLs in memory
// 2. Results are output immediately as they complete
// 3. Can handle infinite streams or files larger than available RAM
// 4. Early termination possible (can stop processing if needed)
func streamHealthCheck(r io.Reader, w io.Writer) int {
	return streamHealthCheckWithContext(context.Background(), r, w)
}

func streamHealthCheckWithContext(ctx context.Context, r io.Reader, w io.Writer) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Channel pipeline: Reader -> URL channel -> Result channel -> Writer
	urlChan := make(chan string, MaxConcurrentRequests)
	resultChan := make(chan Result, MaxConcurrentRequests)

	// Producer: Read URLs from file and send to channel
	go func() {
		defer close(urlChan)
		scanner := bufio.NewScanner(r)
		lineNum := 0

		for scanner.Scan() {
			lineNum++
			url := scanner.Text()

			// Skip empty lines
			if url == "" {
				continue
			}

			// Validate URL - skip invalid URLs with warning
			if !isValidURL(url) {
				fmt.Fprintf(w, "Line %d: Invalid URL: %s (only HTTP/HTTPS allowed)\n", lineNum, url)
				continue
			}

			select {
			case urlChan <- url:
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		}
	}()

	// Workers: Process URLs concurrently (MaxConcurrentRequests workers)
	var wg sync.WaitGroup
	for i := 0; i < MaxConcurrentRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for url := range urlChan {
				result := checkURL(ctx, url)

				select {
				case resultChan <- result:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Close result channel when all workers done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Consumer: Output results immediately
	for result := range resultChan {
		if result.Err != nil {
			fmt.Fprintf(w, "Url: %s; Error: %s\n", result.URL, result.Err)
		} else {
			fmt.Fprintf(w, "Url: %s; Status: %d; Latency: %s\n",
				result.URL, result.Status, result.Latency.Round(time.Millisecond))
		}
	}

	return ExitSuccess
}

// checkURL performs a single URL health check
func checkURL(ctx context.Context, url string) Result {
	var result Result
	result.URL = url
	start := time.Now()

	reqCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		result.Err = err
		result.Latency = time.Since(start)
		return result
	}

	req.Header.Set("User-Agent", UserAgent)

	resp, err := httpClient.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Err = err
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("Warning: failed to close response body for %s: %v", url, cerr)
		}
		result.Status = resp.StatusCode
	}

	return result
}

// HealthCheck reports if a list of web services is up and running.
func HealthCheck(urls []string) []Result {
	results := make([]Result, len(urls))

	// Concurrency limiter using buffered semaphore channel
	sem := make(chan struct{}, min(MaxConcurrentRequests, len(urls)))

	var wg sync.WaitGroup
	wg.Add(len(urls))
	for i := range len(urls) { // Go 1.23+ range over int
		// Acquire semaphore BEFORE creating goroutine (blocks if full)
		sem <- struct{}{}
		go func(idx int, targetURL string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore when done

			var result Result
			result.URL = targetURL
			start := time.Now()

			ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
			if err != nil {
				result.Err = err
				result.Latency = time.Since(start)
				results[idx] = result // No mutex needed - unique index
				return
			}

			// Set User-Agent header
			req.Header.Set("User-Agent", UserAgent)

			// Use shared HTTP client
			resp, err := httpClient.Do(req)
			result.Latency = time.Since(start)
			if err != nil {
				result.Err = err
			} else {
				// Always drain the body to allow connection reuse
				_, _ = io.Copy(io.Discard, resp.Body)
				if cerr := resp.Body.Close(); cerr != nil {
					log.Printf("Warning: failed to close response body for %s: %v", targetURL, cerr)
				}
				result.Status = resp.StatusCode
			}
			results[idx] = result
		}(i, urls[i])
	}

	wg.Wait()
	return results
}

// validateExecution ensures the program runs with appropriate privileges
func validateExecution() {
	switch {
	case osGeteuid() != osGetuid():
		fatal("SUID/SGID execution denied")
	case osGetenv("SUDO_UID") != "":
		fatal(fmt.Sprintf("sudo execution denied (user: %s)", osGetenv("SUDO_USER")))
	}
}

// fatal prints an error and exits
func fatal(msg string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	osExit(ExitError)
}

// isValidURL checks if a string is a valid HTTP/HTTPS URL
func isValidURL(s string) bool {
	return len(s) > MinHTTPURLLength && (s[:MinHTTPURLLength] == HTTPScheme ||
		(len(s) > MinHTTPSURLLength && s[:MinHTTPSURLLength] == HTTPSScheme))
}

// GetServices reads each line of the input reader and returns a list of URLs.
func GetServices(r io.Reader) []string {
	urls := make([]string, 0, 100) // Pre-allocate for better performance
	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		urls = append(urls, scanner.Text())
	}
	return urls
}
