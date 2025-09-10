package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type Result struct {
	Url     string
	Status  int
	Err     error
	Latency time.Duration
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "missing file argument")
		os.Exit(1)
	}

	path := os.Args[1]
	fmt.Printf("Opening %s\n", path)

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	services := GetServices(f)
	results := HealthCheck(services)
	for _, res := range results {
		if res.Err != nil {
			fmt.Printf("Url: %s; Error: %s\n", res.Url, res.Err)
			continue
		}
		fmt.Printf("Url: %s; Status: %d; Latency: %s\n", res.Url, res.Status, res.Latency.Round(time.Millisecond))
	}
}

// HealthCheck report if a list of web service is up and running.
func HealthCheck(urls []string) []Result {
	results := make([]Result, 0, len(urls))

	var wg sync.WaitGroup
	wg.Add(len(urls))
	for _, url := range urls {
		go func() {
			defer wg.Done()
			var result Result
			start := time.Now()
			resp, err := http.Get(url)
			if err != nil {
				result.Err = err
			} else {
				result.Status = resp.StatusCode
				result.Url = url
				result.Latency = time.Since(start)
			}
			results = append(results, result)
		}()
	}

	wg.Wait()
	return results
}

// GetServices read each line of the input reader and return a list of url.
func GetServices(r io.Reader) []string {
	urls := make([]string, 0)
	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		urls = append(urls, scanner.Text())
	}
	return urls
}
