package main

import (
	"net/http"
	"time"
)

var httpClient *http.Client

func initHTTPClient() {
	if cfg == nil {
		httpClient = &http.Client{
			Timeout: 600 * time.Second,
		}
		return
	}

	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 600
	}

	httpClient = &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.MaxIdleConns,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
			ResponseHeaderTimeout: 120 * time.Second,
		},
	}
}
