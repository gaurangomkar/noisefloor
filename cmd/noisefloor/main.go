package main

import (
	"os"

	"github.com/gaurangomkar/noisefloor/internal/cli"
)

// Set with -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	cli.Version = version
	cli.Commit = commit
	cli.Date = date
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
