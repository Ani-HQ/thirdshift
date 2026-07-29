package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/anianroid/thirdshift/internal/node/hardware"
	"github.com/anianroid/thirdshift/internal/node/local"
	"github.com/anianroid/thirdshift/internal/shared/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println(version.Version)
		return nil
	}
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "doctor":
		return runDoctor(args[1:])
	case "run-local":
		return runLocal(args[1:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "write JSON output")
	diskPath := fs.String("disk-path", ".", "path used for free disk check")
	httpsURL := fs.String("https-url", os.Getenv("THIRDSHIFT_DOCTOR_HTTPS_URL"), "HTTPS URL used for outbound reachability")
	wssURL := fs.String("wss-url", os.Getenv("THIRDSHIFT_DOCTOR_WSS_URL"), "WSS URL used for outbound reachability")
	timeout := fs.Duration("timeout", 5*time.Second, "network check timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report := hardware.RunDoctor(context.Background(), hardware.DoctorOptions{
		DiskPath: *diskPath,
		HTTPSURL: *httpsURL,
		WSSURL:   *wssURL,
		Timeout:  *timeout,
	})
	if *jsonOutput {
		return hardware.WriteJSON(os.Stdout, report)
	}
	hardware.WriteHuman(os.Stdout, report)
	return nil
}

func runLocal(args []string) error {
	fs := flag.NewFlagSet("run-local", flag.ContinueOnError)
	modelID := fs.String("model", "", "model id from models/catalog")
	prompt := fs.String("prompt", "", "prompt text")
	catalogDir := fs.String("catalog-dir", "models/catalog", "model catalog directory")
	dataDir := fs.String("data-dir", "", "runtime and model cache directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return local.Run(ctx, local.RunOptions{
		ModelID:    *modelID,
		Prompt:     *prompt,
		CatalogDir: *catalogDir,
		DataDir:    *dataDir,
		Output:     os.Stdout,
	})
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, "thirdshift %s\n", version.Version)
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  thirdshift doctor [--json]")
	fmt.Fprintln(w, "  thirdshift run-local --model <model-id> --prompt <text>")
}
