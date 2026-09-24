// homebrew-deploy provides local metadata-only inspection, never deployment.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"

	"github.com/amg-rfid/amg-rfid-gateway/internal/deploy/homebrew"
)

func run(args []string, output io.Writer) int { return runWith(args, output, homebrew.Inspect) }
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
