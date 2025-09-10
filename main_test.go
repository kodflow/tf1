package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/exp/slices"
)

var services = `https://stackoverflow.com
https://www.google.com
https://go.dev
https://www.docker.com
https://kubernetes.io
https://www.finconsgroup.com
`

func TestHealthCheck(t *testing.T) {
	t.Run("semaphore limits to 64 concurrent requests", func(t *testing.T) {
		// Track concurrent requests
		var activeRequests int32
		var maxConcurrent int32
		var mu sync.Mutex
		
		// Create test server that tracks concurrent requests
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			activeRequests++
			if activeRequests > maxConcurrent {
				maxConcurrent = activeRequests
			}
			mu.Unlock()
			
			// Simulate work
			time.Sleep(100 * time.Millisecond)
			
			mu.Lock()
			activeRequests--
			mu.Unlock()
			
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		
		// Create 100 URLs (more than semaphore size of 64)
		urls := make([]string, 100)
		for i := 0; i < 100; i++ {
			urls[i] = server.URL
		}
		
		// Run health check
		results := HealthCheck(urls)
		
		// Verify all requests completed
		if len(results) != 100 {
			t.Fatalf("expected 100 results, got %d", len(results))
		}
		
		// Verify semaphore limited concurrency to 64
		if maxConcurrent > 64 {
			t.Errorf("expected max concurrent requests <= 64, got %d", maxConcurrent)
		}
		
		// Verify at least some concurrency happened
		if maxConcurrent < 2 {
			t.Errorf("expected some concurrency, but max concurrent was only %d", maxConcurrent)
		}
		
		t.Logf("Max concurrent requests: %d (should be <= 64)", maxConcurrent)
	})
	
	t.Run("no deadlock with exactly 64 URLs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		
		// Exactly 64 URLs (same as semaphore size)
		urls := make([]string, 64)
		for i := 0; i < 64; i++ {
			urls[i] = server.URL
		}
		
		done := make(chan bool, 1)
		go func() {
			results := HealthCheck(urls)
			if len(results) != 64 {
				t.Errorf("expected 64 results, got %d", len(results))
			}
			done <- true
		}()
		
		select {
		case <-done:
			// Success - no deadlock
		case <-time.After(5 * time.Second):
			t.Fatal("deadlock detected: HealthCheck did not complete within 5 seconds")
		}
	})
	
	t.Run("no deadlock with more URLs than semaphore size", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		
		// 200 URLs (much more than semaphore size of 64)
		urls := make([]string, 200)
		for i := 0; i < 200; i++ {
			urls[i] = server.URL
		}
		
		done := make(chan bool, 1)
		go func() {
			results := HealthCheck(urls)
			if len(results) != 200 {
				t.Errorf("expected 200 results, got %d", len(results))
			}
			done <- true
		}()
		
		select {
		case <-done:
			// Success - no deadlock
		case <-time.After(10 * time.Second):
			t.Fatal("deadlock detected: HealthCheck did not complete within 10 seconds")
		}
	})
	
	t.Run("nominal case - single URL returns 200", func(t *testing.T) {
		// Create a test server that returns 200 OK
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		urls := []string{server.URL}
		results := HealthCheck(urls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Status != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, results[0].Status)
		}

		if results[0].URL != server.URL {
			t.Errorf("expected URL %s, got %s", server.URL, results[0].URL)
		}

		if results[0].Err != nil {
			t.Errorf("expected no error, got %v", results[0].Err)
		}

		if results[0].Latency <= 0 {
			t.Errorf("expected positive latency, got %v", results[0].Latency)
		}
	})

	t.Run("nominal case - multiple URLs with different status codes", func(t *testing.T) {
		// Create test servers with different responses
		server200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server200.Close()

		server404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server404.Close()

		server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server500.Close()

		urls := []string{server200.URL, server404.URL, server500.URL}
		results := HealthCheck(urls)

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}

		// Map results by URL for easier verification
		resultMap := make(map[string]Result)
		for _, r := range results {
			resultMap[r.URL] = r
		}

		// Check server200
		if r, ok := resultMap[server200.URL]; ok {
			if r.Status != http.StatusOK {
				t.Errorf("server200: expected status %d, got %d", http.StatusOK, r.Status)
			}
			if r.Err != nil {
				t.Errorf("server200: expected no error, got %v", r.Err)
			}
		} else {
			t.Errorf("server200 result not found")
		}

		// Check server404
		if r, ok := resultMap[server404.URL]; ok {
			if r.Status != http.StatusNotFound {
				t.Errorf("server404: expected status %d, got %d", http.StatusNotFound, r.Status)
			}
			if r.Err != nil {
				t.Errorf("server404: expected no error, got %v", r.Err)
			}
		} else {
			t.Errorf("server404 result not found")
		}

		// Check server500
		if r, ok := resultMap[server500.URL]; ok {
			if r.Status != http.StatusInternalServerError {
				t.Errorf("server500: expected status %d, got %d", http.StatusInternalServerError, r.Status)
			}
			if r.Err != nil {
				t.Errorf("server500: expected no error, got %v", r.Err)
			}
		} else {
			t.Errorf("server500 result not found")
		}
	})

	t.Run("edge case - empty URL list", func(t *testing.T) {
		urls := []string{}
		results := HealthCheck(urls)

		if len(results) != 0 {
			t.Errorf("expected 0 results for empty URL list, got %d", len(results))
		}
	})

	t.Run("edge case - single empty URL string", func(t *testing.T) {
		urls := []string{""}
		results := HealthCheck(urls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Err == nil {
			t.Errorf("expected error for empty URL, got nil")
		}

		if results[0].Status != 0 {
			t.Errorf("expected status 0 for error case, got %d", results[0].Status)
		}
	})

	t.Run("edge case - invalid URL format", func(t *testing.T) {
		invalidURLs := []string{
			"not-a-url",
			"ftp://invalid-protocol.com",
			"://missing-scheme",
			"http://",
			"https://",
		}

		for _, url := range invalidURLs {
			t.Run(fmt.Sprintf("invalid URL: %s", url), func(t *testing.T) {
				results := HealthCheck([]string{url})

				if len(results) != 1 {
					t.Fatalf("expected 1 result, got %d", len(results))
				}

				if results[0].Err == nil {
					t.Errorf("expected error for invalid URL %s, got nil", url)
				}

				if results[0].Status != 0 {
					t.Errorf("expected status 0 for error case, got %d", results[0].Status)
				}
			})
		}
	})

	t.Run("edge case - unreachable host", func(t *testing.T) {
		// Use a non-routable IP address
		urls := []string{"http://192.0.2.1:12345/unreachable"}
		results := HealthCheck(urls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Err == nil {
			t.Errorf("expected error for unreachable host, got nil")
		}

		if results[0].Status != 0 {
			t.Errorf("expected status 0 for error case, got %d", results[0].Status)
		}

		// URL should now be set even when there's an error
		if results[0].URL != urls[0] {
			t.Errorf("expected URL %s, got %s", urls[0], results[0].URL)
		}
	})

	t.Run("edge case - server with slow response", func(t *testing.T) {
		// Create a server that delays response
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		urls := []string{server.URL}
		start := time.Now()
		results := HealthCheck(urls)
		elapsed := time.Since(start)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Status != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, results[0].Status)
		}

		// Check that latency is at least the delay we introduced
		if results[0].Latency < 100*time.Millisecond {
			t.Errorf("expected latency >= 100ms, got %v", results[0].Latency)
		}

		// Check total elapsed time
		if elapsed < 100*time.Millisecond {
			t.Errorf("expected total time >= 100ms, got %v", elapsed)
		}
	})

	t.Run("performance - concurrent requests", func(t *testing.T) {
		// Create multiple test servers with delays
		numServers := 5
		servers := make([]*httptest.Server, numServers)
		urls := make([]string, numServers)

		for i := 0; i < numServers; i++ {
			delay := time.Duration(i*50) * time.Millisecond
			server := httptest.NewServer(http.HandlerFunc(func(d time.Duration) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(d)
					w.WriteHeader(http.StatusOK)
				}
			}(delay)))
			servers[i] = server
			urls[i] = server.URL
			defer server.Close()
		}

		start := time.Now()
		results := HealthCheck(urls)
		elapsed := time.Since(start)

		if len(results) != numServers {
			t.Fatalf("expected %d results, got %d", numServers, len(results))
		}

		// All requests should complete successfully
		for i, r := range results {
			if r.Status != http.StatusOK {
				t.Errorf("server %d: expected status %d, got %d", i, http.StatusOK, r.Status)
			}
			if r.Err != nil {
				t.Errorf("server %d: expected no error, got %v", i, r.Err)
			}
		}

		// Due to concurrency, total time should be less than sum of all delays
		totalDelay := time.Duration(0)
		for i := 0; i < numServers; i++ {
			totalDelay += time.Duration(i*50) * time.Millisecond
		}

		if elapsed >= totalDelay {
			t.Errorf("expected concurrent execution (time < %v), got %v", totalDelay, elapsed)
		}
	})

	t.Run("edge case - duplicate URLs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Same URL repeated multiple times
		urls := []string{server.URL, server.URL, server.URL}
		results := HealthCheck(urls)

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}

		// All should return OK
		for i, r := range results {
			if r.Status != http.StatusOK {
				t.Errorf("result %d: expected status %d, got %d", i, http.StatusOK, r.Status)
			}
			if r.URL != server.URL {
				t.Errorf("result %d: expected URL %s, got %s", i, server.URL, r.URL)
			}
		}
	})

	t.Run("edge case - mixed valid and invalid URLs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		urls := []string{
			server.URL,
			"invalid-url",
			"http://192.0.2.1:12345/unreachable",
			server.URL,
		}

		results := HealthCheck(urls)

		if len(results) != 4 {
			t.Fatalf("expected 4 results, got %d", len(results))
		}

		// Count successes and failures
		successCount := 0
		errorCount := 0
		for _, r := range results {
			if r.Err != nil {
				errorCount++
			} else if r.Status == http.StatusOK {
				successCount++
			}
		}

		if successCount != 2 {
			t.Errorf("expected 2 successful requests, got %d", successCount)
		}

		if errorCount != 2 {
			t.Errorf("expected 2 failed requests, got %d", errorCount)
		}
	})

	t.Run("edge case - server closes connection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close connection immediately
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
		}))
		defer server.Close()

		urls := []string{server.URL}
		results := HealthCheck(urls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Err == nil {
			t.Errorf("expected error for closed connection, got nil")
		}

		if results[0].Status != 0 {
			t.Errorf("expected status 0 for error case, got %d", results[0].Status)
		}
	})

	t.Run("edge case - redirect responses", func(t *testing.T) {
		redirectCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if redirectCount < 2 {
				redirectCount++
				http.Redirect(w, r, "/redirect"+fmt.Sprint(redirectCount), http.StatusFound)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		urls := []string{server.URL}
		results := HealthCheck(urls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		// HTTP client should follow redirects and return final status
		if results[0].Status != http.StatusOK {
			t.Errorf("expected final status %d after redirects, got %d", http.StatusOK, results[0].Status)
		}

		if results[0].Err != nil {
			t.Errorf("expected no error, got %v", results[0].Err)
		}
	})

	t.Run("edge case - very long URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create a very long query parameter
		longParam := strings.Repeat("a", 10000)
		longURL := fmt.Sprintf("%s/?param=%s", server.URL, longParam)

		urls := []string{longURL}
		results := HealthCheck(urls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Status != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, results[0].Status)
		}

		if results[0].URL != longURL {
			t.Errorf("URL mismatch")
		}
	})

	t.Run("edge case - special characters in URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// URLs with special characters
		specialURLs := []string{
			server.URL + "/path with spaces",
			server.URL + "/path?query=value&other=test",
			server.URL + "/path#fragment",
			server.URL + "/üñíçødé",
		}

		for _, url := range specialURLs {
			t.Run(fmt.Sprintf("URL: %s", url), func(t *testing.T) {
				results := HealthCheck([]string{url})

				if len(results) != 1 {
					t.Fatalf("expected 1 result, got %d", len(results))
				}

				// Should handle special characters gracefully
				if results[0].Status != http.StatusOK && results[0].Err == nil {
					t.Errorf("unexpected status %d for URL with special characters", results[0].Status)
				}
			})
		}
	})
}

func TestGetServices(t *testing.T) {
	want := []string{
		"https://stackoverflow.com",
		"https://www.google.com",
		"https://go.dev",
		"https://www.docker.com",
		"https://kubernetes.io",
		"https://www.finconsgroup.com",
	}

	got := GetServices(strings.NewReader(services))
	if slices.Compare(want, got) != 0 {
		t.Errorf("want: %v; got: %v", want, got)
	}
}

func TestValidateExecution(t *testing.T) {
	t.Run("normal execution - no error", func(t *testing.T) {
		// Save original functions
		originalGeteuid := osGeteuid
		originalGetuid := osGetuid
		originalGetenv := osGetenv
		originalExit := osExit
		
		// Track if exit was called
		exitCalled := false
		osExit = func(code int) {
			exitCalled = true
		}
		
		// Restore after test
		defer func() {
			osGeteuid = originalGeteuid
			osGetuid = originalGetuid
			osGetenv = originalGetenv
			osExit = originalExit
		}()
		
		// Mock normal execution (same uid/euid)
		osGeteuid = func() int { return 1000 }
		osGetuid = func() int { return 1000 }
		osGetenv = func(key string) string { return "" }
		
		validateExecution()
		
		if exitCalled {
			t.Error("exit should not be called for normal execution")
		}
	})
	
	t.Run("SUID execution - should fatal", func(t *testing.T) {
		// Save original functions
		originalGeteuid := osGeteuid
		originalGetuid := osGetuid
		originalExit := osExit
		
		// Track if exit was called
		exitCalled := false
		osExit = func(code int) {
			exitCalled = true
		}
		
		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		
		// Restore after test
		defer func() {
			osGeteuid = originalGeteuid
			osGetuid = originalGetuid
			osExit = originalExit
			w.Close()
			os.Stderr = oldStderr
		}()
		
		// Mock SUID execution (different uid/euid)
		osGeteuid = func() int { return 0 }
		osGetuid = func() int { return 1000 }
		
		validateExecution()
		
		// Read captured output
		w.Close()
		var buf bytes.Buffer
		io.Copy(&buf, r)
		
		if !exitCalled {
			t.Error("exit should be called for SUID execution")
		}
		
		expected := "Error: SUID/SGID execution denied\n"
		if buf.String() != expected {
			t.Errorf("expected stderr %q, got %q", expected, buf.String())
		}
	})
	
	t.Run("sudo execution - should fatal", func(t *testing.T) {
		// Save original functions
		originalGeteuid := osGeteuid
		originalGetuid := osGetuid
		originalGetenv := osGetenv
		originalExit := osExit
		
		// Track if exit was called
		exitCalled := false
		osExit = func(code int) {
			exitCalled = true
		}
		
		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		
		// Restore after test
		defer func() {
			osGeteuid = originalGeteuid
			osGetuid = originalGetuid
			osGetenv = originalGetenv
			osExit = originalExit
			w.Close()
			os.Stderr = oldStderr
		}()
		
		// Mock sudo execution
		osGeteuid = func() int { return 1000 }
		osGetuid = func() int { return 1000 }
		osGetenv = func(key string) string {
			if key == "SUDO_UID" {
				return "1001"
			}
			if key == "SUDO_USER" {
				return "testuser"
			}
			return ""
		}
		
		validateExecution()
		
		// Read captured output
		w.Close()
		var buf bytes.Buffer
		io.Copy(&buf, r)
		
		if !exitCalled {
			t.Error("exit should be called for sudo execution")
		}
		
		expectedMsg := "Error: sudo execution denied (user: testuser)\n"
		if buf.String() != expectedMsg {
			t.Errorf("expected %q, got %q", expectedMsg, buf.String())
		}
	})
}

func TestFatal(t *testing.T) {
	// Save original osExit
	originalExit := osExit
	
	// Track exit code
	var exitCode int
	exitCalled := false
	osExit = func(code int) {
		exitCode = code
		exitCalled = true
	}
	
	// Restore after test
	defer func() {
		osExit = originalExit
	}()
	
	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	
	// Run fatal
	fatal("test error message")
	
	// Restore stderr
	w.Close()
	os.Stderr = oldStderr
	
	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	
	// Check output
	expected := "Error: test error message\n"
	if buf.String() != expected {
		t.Errorf("expected stderr %q, got %q", expected, buf.String())
	}
	
	// Check exit was called with code 1
	if !exitCalled || exitCode != 1 {
		t.Errorf("expected osExit(1) to be called, exitCalled=%v, exitCode=%d", exitCalled, exitCode)
	}
}

func TestRun(t *testing.T) {
	t.Run("no arguments - should return 1", func(t *testing.T) {
		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		
		exitCode := run([]string{"healthcheck"})
		
		w.Close()
		os.Stderr = oldStderr
		
		var buf bytes.Buffer
		io.Copy(&buf, r)
		
		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}
		
		if !strings.Contains(buf.String(), "missing file argument") {
			t.Errorf("expected 'missing file argument' in stderr, got %q", buf.String())
		}
	})
	
	t.Run("nonexistent file - should return 1", func(t *testing.T) {
		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		
		exitCode := run([]string{"healthcheck", "/nonexistent/file.txt"})
		
		w.Close()
		os.Stderr = oldStderr
		
		var buf bytes.Buffer
		io.Copy(&buf, r)
		
		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}
		
		if !strings.Contains(buf.String(), "no such file or directory") {
			t.Errorf("expected 'no such file or directory' in stderr, got %q", buf.String())
		}
	})
	
	t.Run("invalid URL in file - should return 1", func(t *testing.T) {
		// Create temp file with invalid URL
		tmpfile, err := os.CreateTemp("", "test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpfile.Name())
		
		if _, err := tmpfile.Write([]byte("ftp://invalid.com\n")); err != nil {
			t.Fatal(err)
		}
		tmpfile.Close()
		
		// Capture stderr and stdout
		oldStderr := os.Stderr
		oldStdout := os.Stdout
		rErr, wErr, _ := os.Pipe()
		rOut, wOut, _ := os.Pipe()
		os.Stderr = wErr
		os.Stdout = wOut
		
		exitCode := run([]string{"healthcheck", tmpfile.Name()})
		
		wErr.Close()
		wOut.Close()
		os.Stderr = oldStderr
		os.Stdout = oldStdout
		
		var bufErr bytes.Buffer
		var bufOut bytes.Buffer
		io.Copy(&bufErr, rErr)
		io.Copy(&bufOut, rOut)
		
		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}
		
		if !strings.Contains(bufErr.String(), "Invalid URL") && !strings.Contains(bufErr.String(), "only HTTP/HTTPS allowed") {
			t.Errorf("expected 'Invalid URL' error in stderr, got %q", bufErr.String())
		}
		
		if !strings.Contains(bufOut.String(), "Opening "+tmpfile.Name()) {
			t.Errorf("expected 'Opening' message in stdout, got %q", bufOut.String())
		}
	})
	
	t.Run("valid execution with test file - should return 0", func(t *testing.T) {
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		
		// Create temp file with test server URL
		tmpfile, err := os.CreateTemp("", "test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpfile.Name())
		
		if _, err := tmpfile.Write([]byte(server.URL + "\n")); err != nil {
			t.Fatal(err)
		}
		tmpfile.Close()
		
		// Capture stdout
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		
		exitCode := run([]string{"healthcheck", tmpfile.Name()})
		
		w.Close()
		os.Stdout = oldStdout
		
		var buf bytes.Buffer
		io.Copy(&buf, r)
		
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}
		
		output := buf.String()
		if !strings.Contains(output, "Opening "+tmpfile.Name()) {
			t.Errorf("expected 'Opening' message, got %q", output)
		}
		
		if !strings.Contains(output, "Status: 200") {
			t.Errorf("expected 'Status: 200' in output, got %q", output)
		}
		
		if !strings.Contains(output, server.URL) {
			t.Errorf("expected server URL in output, got %q", output)
		}
	})
	
	t.Run("file with error URLs - should return 0 but show errors", func(t *testing.T) {
		// Create temp file with unreachable URL
		tmpfile, err := os.CreateTemp("", "test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpfile.Name())
		
		if _, err := tmpfile.Write([]byte("http://192.0.2.1:12345/unreachable\n")); err != nil {
			t.Fatal(err)
		}
		tmpfile.Close()
		
		// Capture stdout
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		
		exitCode := run([]string{"healthcheck", tmpfile.Name()})
		
		w.Close()
		os.Stdout = oldStdout
		
		var buf bytes.Buffer
		io.Copy(&buf, r)
		
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}
		
		output := buf.String()
		if !strings.Contains(output, "Error:") {
			t.Errorf("expected 'Error:' in output, got %q", output)
		}
	})
}

func TestMain(t *testing.T) {
	// Test that main calls osExit with the return value from run
	originalExit := osExit
	originalArgs := os.Args
	
	var exitCode int
	osExit = func(code int) {
		exitCode = code
	}
	
	defer func() {
		osExit = originalExit
		os.Args = originalArgs
	}()
	
	// Set args to trigger missing file error
	os.Args = []string{"healthcheck"}
	
	// Capture stderr to avoid polluting test output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	
	main()
	
	w.Close()
	os.Stderr = oldStderr
	io.Copy(&bytes.Buffer{}, r)
	
	if exitCode != 1 {
		t.Errorf("expected main to call osExit(1), got osExit(%d)", exitCode)
	}
}

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		valid bool
	}{
		// Valid URLs
		{"valid http", "http://example.com", true},
		{"valid https", "https://example.com", true},
		{"valid http with path", "http://example.com/path", true},
		{"valid https with path", "https://example.com/path", true},
		{"valid http with port", "http://example.com:8080", true},
		{"valid https with port", "https://example.com:443", true},
		{"valid http minimum", "http://a", true},
		{"valid https minimum", "https://a", true},
		
		// Invalid URLs
		{"empty string", "", false},
		{"no protocol", "example.com", false},
		{"ftp protocol", "ftp://example.com", false},
		{"file protocol", "file:///path", false},
		{"just http", "http://", false},
		{"just https", "https://", false},
		{"http without slash", "http:/", false},
		{"https without slash", "https:/", false},
		{"http missing colon", "http//example.com", false},
		{"https missing colon", "https//example.com", false},
		{"short string", "http", false},
		{"almost http", "http:/", false},
		{"almost https", "https:", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidURL(tt.url)
			if got != tt.valid {
				t.Errorf("isValidURL(%q) = %v, want %v", tt.url, got, tt.valid)
			}
		})
	}
}
