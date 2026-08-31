// Command wayminder-healthcheck checks the local Wayminder readiness endpoint.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	url := "http://127.0.0.1:8080/readyz"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintln(os.Stderr, resp.Status)
		os.Exit(1)
	}
}
