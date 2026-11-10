package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tkilb/lazyprint/internal/config"
	"github.com/tkilb/lazyprint/internal/runner"
	"github.com/tkilb/lazyprint/internal/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	configFlag := flag.String("config", "", "path to config file")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("lazyprint %s\n", version.Version)
		return
	}

	cfg, err := config.Load(*configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	_ = cfg
	_ = runner.NewRealRunner()

	fmt.Println("lazyprint - CUPS & SANE TUI")
}
