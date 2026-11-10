package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tkilb/lazyprint/internal/config"
	"github.com/tkilb/lazyprint/internal/runner"
	"github.com/tkilb/lazyprint/internal/ui/app"
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

	r := runner.NewRealRunner()
	model := app.NewModel(cfg, r)

	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
	}
	if cfg.UI.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}

	p := tea.NewProgram(model, opts...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running lazyprint: %v\n", err)
		os.Exit(1)
	}
}
