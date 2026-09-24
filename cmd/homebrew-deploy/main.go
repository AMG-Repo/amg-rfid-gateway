// homebrew-deploy provides local metadata-only inspection, never deployment.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"

	"github.com/amg-rfid/amg-rfid-gateway/internal/deploy/homebrew"
)

func run(args []string, output io.Writer) int {
	if len(args) > 0 && args[0] == "plan" {
		if json.NewEncoder(output).Encode(homebrew.ContentObservation{Code: "unsupported_action", RuntimeConfig: "NOT VERIFIED", SystemdTrust: "NOT VERIFIED", IdentityContinuity: "NOT PROVED"}) != nil {
			return 1
		}
		return 2
	}
	if len(args) > 0 && args[0] == "content-check" {
		return runContentCheck(args[1:], output)
	}
	return runWith(args, output, homebrew.Inspect)
}
func runContentCheck(args []string, output io.Writer) int {
	return runContentCheckWith(args, output, homebrew.CheckContent)
}
func runContentCheckWith(args []string, output io.Writer, verify func(homebrew.Profile, string, string) homebrew.ContentObservation) int {
	flags := flag.NewFlagSet("content-check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pkg := flags.String("package", "", "package path")
	cfg := flags.String("config", "", "config path")
	dev := flags.Uint64("package-device", 0, "device")
	ino := flags.Uint64("package-inode", 0, "inode")
	version := flags.String("version", "", "release version")
	arch := flags.String("arch", "", "release architecture")
	r := homebrew.ContentObservation{Code: "invalid_profile", RuntimeConfig: "NOT VERIFIED", SystemdTrust: "NOT VERIFIED", IdentityContinuity: "NOT PROVED"}
	if flags.Parse(args) == nil && flags.NArg() == 0 {
		r = verify(homebrew.Profile{Package: *pkg, Config: *cfg, PackageIdentity: homebrew.Identity{Device: *dev, Inode: *ino}}, *version, *arch)
	}
	if json.NewEncoder(output).Encode(r) != nil {
		return 1
	}
	if r.Code != "content_equal" {
		return 2
	}
	return 0
}
func runWith(args []string, output io.Writer, inspect func(homebrew.Profile) homebrew.Result) int {
	flags := flag.NewFlagSet("homebrew-deploy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pkg := flags.String("package", "", "absolute package file path")
	cfg := flags.String("config", "", "absolute config file path")
	dev := flags.Uint64("package-device", 0, "expected package device")
	ino := flags.Uint64("package-inode", 0, "expected package inode")
	r := homebrew.Result{Code: "invalid_profile", RuntimeConfig: "NOT VERIFIED", SystemdTrust: "NOT VERIFIED"}
	if flags.Parse(args) == nil && flags.NArg() == 0 {
		r = inspect(homebrew.Profile{Package: *pkg, Config: *cfg, PackageIdentity: homebrew.Identity{Device: *dev, Inode: *ino}})
	}
	if json.NewEncoder(output).Encode(r) != nil {
		return 1
	}
	if r.Code != "inspection_only" {
		return 2
	}
	return 0
}
func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
