package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallUpdatePreservesOriginalOnFailure(t *testing.T) {
	for _, mode := range []string{"success", "http-error", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http-error" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if mode == "truncated" {
					w.Header().Set("Content-Length", "100")
				}
				fmt.Fprint(w, "new executable")
			}))
			defer server.Close()
			exe := filepath.Join(t.TempDir(), "SpaceDot.exe")
			if err := os.WriteFile(exe, []byte("old executable"), 0600); err != nil {
				t.Fatal(err)
			}
			r := &release{}
			r.Assets = append(r.Assets, struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}{Name: assetName, URL: server.URL})
			err := installUpdateAt(exe, r, nil)
			data, readErr := os.ReadFile(exe)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "success" {
				if err != nil || string(data) != "new executable" {
					t.Fatalf("%v %s", err, data)
				}
				old, err := os.ReadFile(exe + ".old")
				if err != nil || string(old) != "old executable" {
					t.Fatal("backup missing", err)
				}
			} else {
				if err == nil || string(data) != "old executable" {
					t.Fatal("original not preserved", err, string(data))
				}
			}
			if _, err := os.Stat(exe + ".download"); !os.IsNotExist(err) {
				t.Fatal("download temporary file left over")
			}
		})
	}
}
