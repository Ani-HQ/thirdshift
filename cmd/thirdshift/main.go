package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	goruntime "runtime"
	"strings"
	"syscall"
	"time"

	nodeagent "github.com/Ani-HQ/thirdshift/internal/node/agent"
	nodeconfig "github.com/Ani-HQ/thirdshift/internal/node/config"
	"github.com/Ani-HQ/thirdshift/internal/node/control"
	"github.com/Ani-HQ/thirdshift/internal/node/hardware"
	"github.com/Ani-HQ/thirdshift/internal/node/identity"
	"github.com/Ani-HQ/thirdshift/internal/node/local"
	nodemodels "github.com/Ani-HQ/thirdshift/internal/node/models"
	noderegistration "github.com/Ani-HQ/thirdshift/internal/node/registration"
	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
	nodeschedule "github.com/Ani-HQ/thirdshift/internal/node/schedule"
	nodeupdate "github.com/Ani-HQ/thirdshift/internal/node/update"
	"github.com/Ani-HQ/thirdshift/internal/shared/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println(version.String())
		return nil
	}
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "doctor":
		return runDoctor(args[1:])
	case "configure":
		return runConfigure(args[1:])
	case "login":
		return runLogin(args[1:])
	case "start":
		return runStart(args[1:])
	case "status":
		return runStatus(args[1:])
	case "pause":
		return runPauseResume("pause", args[1:])
	case "resume":
		return runPauseResume("resume", args[1:])
	case "run-local":
		return runLocal(args[1:])
	case "update":
		return runUpdate(args[1:])
	case "card":
		return runCard(args[1:])
	case "version":
		fmt.Println(version.String())
		return nil
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	invite := fs.String("invite", "", "registration invite token")
	coordinatorURL := fs.String("coordinator", os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "coordinator base URL")
	dataDir := fs.String("data-dir", os.Getenv("THIRDSHIFT_NODE_DATA_DIR"), "node data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := noderegistration.Login(ctx, noderegistration.LoginOptions{
		DataDir:        *dataDir,
		CoordinatorURL: *coordinatorURL,
		InviteToken:    *invite,
	})
	if err != nil {
		return err
	}
	action := "refreshed"
	if result.Registered {
		action = "registered"
	}
	fmt.Fprintf(os.Stdout, "node %s: %s\n", action, result.Credentials.NodeID)
	fmt.Fprintf(os.Stdout, "credentials: %s\n", result.Credentials.CoordinatorURL)
	return nil
}

func runConfigure(args []string) error {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	dataDir := fs.String("data-dir", os.Getenv("THIRDSHIFT_NODE_DATA_DIR"), "node data directory")
	from := fs.String("from", "", "local schedule start in HH:MM")
	until := fs.String("until", "", "local schedule end in HH:MM")
	maxTemp := fs.Int("max-temp", 0, "GPU temperature soft limit in Celsius")
	hardTemp := fs.Int("hard-temp", 0, "GPU temperature hard limit in Celsius")
	hysteresis := fs.Int("thermal-hysteresis", 0, "thermal recovery margin in Celsius")
	pauseIdleTimeout := fs.Duration("pause-idle-timeout", 0, "duration before paused node unloads model")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := nodeconfig.Load(*dataDir)
	if err != nil {
		return err
	}
	if *from != "" {
		cfg.ScheduleFrom = *from
	}
	if *until != "" {
		cfg.ScheduleUntil = *until
	}
	if cfg.ScheduleFrom == "" || cfg.ScheduleUntil == "" {
		return fmt.Errorf("schedule requires both --from and --until in HH:MM")
	}
	if _, err := nodeschedule.ParseWindow(cfg.ScheduleFrom, cfg.ScheduleUntil); err != nil {
		return err
	}
	if *maxTemp > 0 {
		cfg.MaxTempC = *maxTemp
	}
	if *hardTemp > 0 {
		cfg.HardTempC = *hardTemp
	}
	if *hysteresis > 0 {
		cfg.ThermalHysteresis = *hysteresis
	}
	if *pauseIdleTimeout > 0 {
		cfg.PauseIdleTimeout = *pauseIdleTimeout
	}
	if err := nodeconfig.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "schedule: %s-%s\n", cfg.ScheduleFrom, cfg.ScheduleUntil)
	fmt.Fprintf(os.Stdout, "temperature: max=%d C hard=%d C hysteresis=%d C\n", cfg.MaxTempC, cfg.HardTempC, cfg.ThermalHysteresis)
	return nil
}

func runStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	dataDir := fs.String("data-dir", os.Getenv("THIRDSHIFT_NODE_DATA_DIR"), "node data directory")
	coordinatorURL := fs.String("coordinator", os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "coordinator base URL override")
	modelID := fs.String("model", os.Getenv("THIRDSHIFT_MODEL_ID"), "model id, or \"auto\" to pick the largest model this hardware can run")
	catalogDir := fs.String("catalog-dir", "models/catalog", "model catalog directory")
	runtimeBaseURL := fs.String("runtime-base-url", os.Getenv("THIRDSHIFT_RUNTIME_BASE_URL"), "development-only existing loopback llama-compatible runtime URL")
	heartbeatInterval := fs.Duration("heartbeat-interval", 15*time.Second, "heartbeat interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := nodeconfig.Load(*dataDir)
	if err != nil {
		return err
	}
	if *coordinatorURL != "" {
		cfg.CoordinatorURL = *coordinatorURL
	}
	if *modelID != "" {
		cfg.ModelID = *modelID
	}
	login, err := noderegistration.Login(ctx, noderegistration.LoginOptions{
		DataDir:        cfg.DataDir,
		CoordinatorURL: cfg.CoordinatorURL,
	})
	if err != nil {
		return err
	}
	var runtimeProvider nodeagent.RuntimeStatusProvider
	if *runtimeBaseURL != "" {
		runtimeProvider = nodeagent.ExistingRuntimeProvider{CatalogDir: *catalogDir, BaseURL: *runtimeBaseURL}
	}
	// One hardware probe answers both questions: which runtime build to install
	// and which model tier this machine can actually run.
	probeCtx, cancelProbe := context.WithTimeout(ctx, 20*time.Second)
	resources, resourceErr := hardware.DetectHostResources(probeCtx, nil, cfg.DataDir, goruntime.GOOS)
	cancelProbe()
	if resourceErr != nil {
		return fmt.Errorf("detect host resources: %w", resourceErr)
	}
	gpuVendor := resources.GPUVendor

	manifests, err := nodemodels.LoadSelectableManifests(*catalogDir)
	if err != nil {
		return err
	}
	selection, err := nodemodels.ResolveModel(cfg.ModelID, manifests, nodemodels.HostCapacity{
		VRAMTotalMB: resources.VRAMTotalMB,
		RAMTotalMB:  resources.RAMTotalMB,
		DiskFreeMB:  resources.DiskFreeMB,
	})
	if err != nil {
		return err
	}
	cfg.ModelID = selection.ModelID
	if err := nodeconfig.Save(cfg); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "hardware: gpu vendor %s, %s, %dMB RAM, %dMB free disk\n",
		gpuVendor, describeDetectedVRAM(resources), resources.RAMTotalMB, resources.DiskFreeMB)
	fmt.Fprintf(os.Stdout, "selected %s: %s\n", selection.ModelID, selection.Reason)
	if selection.VRAMAssumed {
		fmt.Fprintf(os.Stdout,
			"warning: VRAM was not measured on this host, so the VRAM floor for %s is unverified; if the runtime runs out of memory, rerun with an explicit smaller --model\n",
			selection.ModelID)
	}
	fmt.Fprintf(os.Stdout, "starting node %s\n", login.Credentials.NodeID)
	return nodeagent.Run(ctx, nodeagent.Options{
		DataDir:           cfg.DataDir,
		CatalogDir:        *catalogDir,
		CoordinatorURL:    login.Credentials.CoordinatorURL,
		ModelID:           selection.ModelID,
		AccessToken:       login.Credentials.AccessToken,
		NodeID:            login.Credentials.NodeID,
		HeartbeatInterval: *heartbeatInterval,
		Runtime:           runtimeProvider,
		ScheduleFrom:      cfg.ScheduleFrom,
		ScheduleUntil:     cfg.ScheduleUntil,
		MaxTempC:          cfg.MaxTempC,
		HardTempC:         cfg.HardTempC,
		ThermalHysteresis: cfg.ThermalHysteresis,
		PauseIdleTimeout:  cfg.PauseIdleTimeout,
		GPUVendor:         gpuVendor,
		Output:            os.Stdout,
	})
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dataDir := fs.String("data-dir", os.Getenv("THIRDSHIFT_NODE_DATA_DIR"), "node data directory")
	jsonOutput := fs.Bool("json", false, "write JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := nodeconfig.Load(*dataDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := control.Send(ctx, cfg.DataDir, control.Command{Action: "status"})
	var status *control.Status
	if err == nil {
		status = response.Status
	} else {
		status, err = nodeagent.ReadStatus(cfg.DataDir)
		if err != nil {
			return fmt.Errorf("node is not running and no status file is available")
		}
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(status)
	}
	printStatus(status)
	return nil
}

func runPauseResume(action string, args []string) error {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	dataDir := fs.String("data-dir", os.Getenv("THIRDSHIFT_NODE_DATA_DIR"), "node data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := nodeconfig.Load(*dataDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := control.Send(ctx, cfg.DataDir, control.Command{Action: action})
	if err != nil {
		return err
	}
	if response.Status != nil {
		fmt.Fprintf(os.Stdout, "state: %s\n", response.Status.State)
	}
	return nil
}

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	manifestURL := fs.String("manifest", nodeupdate.DefaultReleaseManifestURL, "release manifest URL or local path")
	installDir := fs.String("install-dir", "", "install directory; defaults to current executable directory")
	currentBinary := fs.String("current-binary", "", "current binary path; test/development override")
	publicKeyEncoded := fs.String("public-key", nodeupdate.DefaultReleasePublicKeyBase64, "base64 Ed25519 release public key")
	platformKey := fs.String("platform", "", "platform key override, such as windows/amd64")
	verifyOnly := fs.Bool("verify-only", false, "verify a downloaded artifact without installing it")
	artifactPath := fs.String("artifact", "", "downloaded artifact path for --verify-only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	publicKey, err := noderuntime.DecodePublicKey(*publicKeyEncoded)
	if err != nil {
		return err
	}
	if *verifyOnly {
		if *artifactPath == "" {
			return fmt.Errorf("--artifact is required with --verify-only")
		}
		manifest, err := nodeupdate.LoadManifestBytes(*manifestURL)
		if err != nil {
			return err
		}
		key := *platformKey
		if key == "" {
			key = runtimePlatformKey()
		}
		if err := nodeupdate.VerifyDownloadedArtifact(manifest, key, *artifactPath, publicKey); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "release artifact verified: %s %s\n", manifest.Version, key)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := (nodeupdate.Manager{
		InstallDir:    *installDir,
		CurrentBinary: *currentBinary,
		PublicKey:     publicKey,
		PlatformKey:   *platformKey,
	}).Update(ctx, *manifestURL)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "updated: %s\ncommit: %s\ninstalled: %s\nprevious: %s\n", result.Version, result.BuildID, result.InstalledPath, result.PreviousPath)
	return nil
}

func runCard(args []string) error {
	fs := flag.NewFlagSet("card", flag.ContinueOnError)
	dataDir := fs.String("data-dir", os.Getenv("THIRDSHIFT_NODE_DATA_DIR"), "node data directory")
	coordinatorURL := fs.String("coordinator", "", "coordinator base URL override")
	nodeID := fs.String("node", "", "node id override")
	jsonOutput := fs.Bool("json", false, "write JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := nodeconfig.Load(*dataDir)
	if err != nil {
		return err
	}
	if *coordinatorURL != "" {
		cfg.CoordinatorURL = *coordinatorURL
	}
	if *nodeID == "" || cfg.CoordinatorURL == "" {
		creds, err := identity.LoadCredentials(cfg.DataDir)
		if err != nil {
			return err
		}
		if *nodeID == "" {
			*nodeID = creds.NodeID
		}
		if cfg.CoordinatorURL == "" {
			cfg.CoordinatorURL = creds.CoordinatorURL
		}
	}
	card, err := fetchContributionCard(context.Background(), cfg.CoordinatorURL, *nodeID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(card)
	}
	fmt.Fprintln(os.Stdout, "Thirdshift contribution card")
	fmt.Fprintf(os.Stdout, "Node: %s\n", card.NodeName)
	fmt.Fprintf(os.Stdout, "Nights active: %d\n", card.NightsActive)
	fmt.Fprintf(os.Stdout, "Jobs accepted: %d\n", card.JobsAccepted)
	fmt.Fprintf(os.Stdout, "Tokens served: %d\n", card.TokensServed)
	fmt.Fprintf(os.Stdout, "Credit earned: %s\n", formatMicrodollars(card.CreditEarnedMicrodollars))
	return nil
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

func printStatus(status *control.Status) {
	fmt.Fprintf(os.Stdout, "node id: %s\n", dash(status.NodeID))
	fmt.Fprintf(os.Stdout, "state: %s\n", dash(status.State))
	fmt.Fprintf(os.Stdout, "gpu: %s\n", dash(status.GPU))
	fmt.Fprintf(os.Stdout, "model: %s\n", dash(status.ModelID))
	fmt.Fprintf(os.Stdout, "runtime hash: %s\n", dash(status.RuntimeHash))
	fmt.Fprintf(os.Stdout, "model hash: %s\n", dash(status.ModelHash))
	fmt.Fprintf(os.Stdout, "schedule: %s\n", dash(status.Schedule))
	fmt.Fprintf(os.Stdout, "schedule state: %s\n", dash(status.ScheduleState))
	fmt.Fprintf(os.Stdout, "thermal state: %s\n", dash(status.ThermalState))
	fmt.Fprintf(os.Stdout, "paused: %t\n", status.Paused)
	fmt.Fprintf(os.Stdout, "draining: %t\n", status.Draining)
	if status.TemperatureC != nil {
		fmt.Fprintf(os.Stdout, "temperature: %d C\n", *status.TemperatureC)
	} else {
		fmt.Fprintln(os.Stdout, "temperature: unavailable")
	}
	if status.PowerW != nil {
		fmt.Fprintf(os.Stdout, "power: %d W\n", *status.PowerW)
	} else {
		fmt.Fprintln(os.Stdout, "power: unavailable")
	}
	connectivity := "disconnected"
	if status.SessionConnected {
		connectivity = "connected"
	}
	fmt.Fprintf(os.Stdout, "session: %s\n", connectivity)
	if status.LastHeartbeatAt != nil {
		fmt.Fprintf(os.Stdout, "last heartbeat: %s\n", status.LastHeartbeatAt.Format(time.RFC3339))
	}
	if status.LastError != "" {
		fmt.Fprintf(os.Stdout, "last error: %s\n", status.LastError)
	}
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

type contributionCard struct {
	NodeID                   string    `json:"node_id"`
	NodeName                 string    `json:"node_name"`
	NightsActive             int64     `json:"nights_active"`
	JobsAccepted             int64     `json:"jobs_accepted"`
	TokensServed             int64     `json:"tokens_served"`
	CreditEarnedMicrodollars int64     `json:"credit_earned_microdollars"`
	GeneratedAt              time.Time `json:"generated_at"`
}

func fetchContributionCard(ctx context.Context, coordinatorURL, nodeID string) (contributionCard, error) {
	if coordinatorURL == "" {
		return contributionCard{}, fmt.Errorf("coordinator URL is required")
	}
	if nodeID == "" {
		return contributionCard{}, fmt.Errorf("node id is required")
	}
	endpoint := strings.TrimRight(coordinatorURL, "/") + "/v1/nodes/" + url.PathEscape(nodeID) + "/card"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return contributionCard{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return contributionCard{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error == "" {
			apiErr.Error = resp.Status
		}
		return contributionCard{}, fmt.Errorf("contribution card request failed: %s", apiErr.Error)
	}
	var card contributionCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return contributionCard{}, fmt.Errorf("decode contribution card: %w", err)
	}
	return card, nil
}

func formatMicrodollars(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	return fmt.Sprintf("%s$%d.%06d", sign, value/1_000_000, value%1_000_000)
}

func runtimePlatformKey() string {
	return goruntime.GOOS + "/" + goruntime.GOARCH
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, "thirdshift %s\n", version.String())
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  thirdshift doctor [--json]")
	fmt.Fprintln(w, "  thirdshift configure --from HH:MM --until HH:MM")
	fmt.Fprintln(w, "  thirdshift login --invite <token> --coordinator <url>")
	fmt.Fprintln(w, "  thirdshift start [--runtime-base-url http://127.0.0.1:<port>]")
	fmt.Fprintln(w, "  thirdshift status [--json]")
	fmt.Fprintln(w, "  thirdshift pause")
	fmt.Fprintln(w, "  thirdshift resume")
	fmt.Fprintln(w, "  thirdshift run-local --model <model-id> --prompt <text>")
	fmt.Fprintln(w, "  thirdshift update --manifest <release-manifest-url>")
	fmt.Fprintln(w, "  thirdshift card [--json]")
	fmt.Fprintln(w, "  thirdshift version")
}

func describeDetectedVRAM(resources hardware.HostResources) string {
	if !resources.VRAMKnown() {
		return "VRAM unknown"
	}
	return fmt.Sprintf("%dMB VRAM", resources.VRAMTotalMB)
}
