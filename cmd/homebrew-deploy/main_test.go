package main

import (
	"bytes"
	"encoding/json"
	"github.com/amg-rfid/amg-rfid-gateway/internal/deploy/homebrew"
	"strings"
	"testing"
)

func TestCLIInspectionOutput(t *testing.T) {
	var out bytes.Buffer
	code := runWith([]string{"-package", "/fixture/package", "-config", "/fixture/config", "-package-device", "2", "-package-inode", "3"}, &out, func(p homebrew.Profile) homebrew.Result {
		if p.PackageIdentity.Device != 2 || p.PackageIdentity.Inode != 3 {
			t.Fatal("identity not forwarded")
		}
		return homebrew.Result{Code: "inspection_only", RuntimeConfig: "NOT VERIFIED", SystemdTrust: "NOT VERIFIED"}
	})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r homebrew.Result
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Code != "inspection_only" {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	if strings.Contains(out.String(), "/fixture") {
		t.Fatal("path disclosure")
	}
}

func TestCLIRefusal(t *testing.T) {
	for _, args := range [][]string{{"-package", "/secret/path", "-unknown", "private-value"}, {"-package", "/secret/path"}, {"-package-device", "not-a-number"}} {
		var out bytes.Buffer
		exit := run(args, &out)
		if exit != 2 {
			t.Fatalf("exit %d", exit)
		}
		var result struct {
			Code          string
			RuntimeConfig string
			SystemdTrust  string
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Code != "invalid_profile" || result.RuntimeConfig != "NOT VERIFIED" || result.SystemdTrust != "NOT VERIFIED" {
			t.Fatalf("unexpected result: %v", result)
		}
		if strings.Contains(out.String(), "private-value") || strings.Contains(out.String(), "/secret/path") {
			t.Fatal("argument disclosure")
		}
	}
}
