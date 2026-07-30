package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/database"
	"github.com/anianroid/thirdshift/internal/coordinator/ledger"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "migrate":
		return migrate(args[1:])
	case "org":
		return org(args[1:])
	case "apikey":
		return apikey(args[1:])
	case "catalog":
		return catalog(args[1:])
	case "invite":
		return invite(args[1:])
	case "fleet":
		return fleet(args[1:])
	case "nodes":
		return nodes(args[1:])
	case "credits":
		return credits(args[1:])
	case "payout":
		return payout(args[1:])
	case "report":
		return report(args[1:])
	case "--help", "-h", "help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}
}

func org(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("org command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "create":
		return orgCreate(args[1:])
	default:
		return fmt.Errorf("unknown org command %q\n\n%s", args[0], usageText())
	}
}

func orgCreate(args []string) error {
	fs := flag.NewFlagSet("org create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	name := fs.String("name", "", "organization name")
	coordinatorURL := fs.String("coordinator", firstNonEmpty(os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "http://127.0.0.1:8080"), "coordinator base URL")
	operatorToken := fs.String("operator-token", os.Getenv("THIRDSHIFT_OPERATOR_TOKEN"), "operator bearer token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}
	if *operatorToken == "" {
		return fmt.Errorf("operator token is required; set THIRDSHIFT_OPERATOR_TOKEN or pass --operator-token")
	}
	var resp struct {
		OrgID string `json:"org_id"`
		Name  string `json:"name"`
	}
	if err := postAdminJSON(*coordinatorURL+"/internal/v1/orgs", *operatorToken, map[string]string{"name": *name}, &resp); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "org_id: %s\nname: %s\n", resp.OrgID, resp.Name)
	return nil
}

func apikey(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("apikey command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "create":
		return apikeyCreate(args[1:])
	default:
		return fmt.Errorf("unknown apikey command %q\n\n%s", args[0], usageText())
	}
}

func apikeyCreate(args []string) error {
	fs := flag.NewFlagSet("apikey create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	orgID := fs.String("org", "", "organization id")
	name := fs.String("name", "default", "API key name")
	modelIDs := multiFlag{}
	fs.Var(&modelIDs, "model", "allowed model id; repeatable")
	coordinatorURL := fs.String("coordinator", firstNonEmpty(os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "http://127.0.0.1:8080"), "coordinator base URL")
	operatorToken := fs.String("operator-token", os.Getenv("THIRDSHIFT_OPERATOR_TOKEN"), "operator bearer token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *orgID == "" {
		return fmt.Errorf("--org is required")
	}
	if *operatorToken == "" {
		return fmt.Errorf("operator token is required; set THIRDSHIFT_OPERATOR_TOKEN or pass --operator-token")
	}
	var resp struct {
		APIKeyID string `json:"api_key_id"`
		Key      string `json:"key"`
	}
	if err := postAdminJSON(*coordinatorURL+"/internal/v1/api-keys", *operatorToken, map[string]any{
		"org_id": *orgID,
		"name":   *name,
		"models": []string(modelIDs),
	}, &resp); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "api_key_id: %s\nkey: %s\n", resp.APIKeyID, resp.Key)
	return nil
}

func catalog(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("catalog command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "sync":
		return catalogSync(args[1:])
	default:
		return fmt.Errorf("unknown catalog command %q\n\n%s", args[0], usageText())
	}
}

func catalogSync(args []string) error {
	fs := flag.NewFlagSet("catalog sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	catalogDir := fs.String("catalog-dir", "models/catalog", "model catalog directory")
	coordinatorURL := fs.String("coordinator", firstNonEmpty(os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "http://127.0.0.1:8080"), "coordinator base URL")
	operatorToken := fs.String("operator-token", os.Getenv("THIRDSHIFT_OPERATOR_TOKEN"), "operator bearer token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *operatorToken == "" {
		return fmt.Errorf("operator token is required; set THIRDSHIFT_OPERATOR_TOKEN or pass --operator-token")
	}
	var resp struct {
		Synced int `json:"synced"`
	}
	if err := postAdminJSON(*coordinatorURL+"/internal/v1/catalog/sync", *operatorToken, map[string]string{"catalog_dir": *catalogDir}, &resp); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "catalog: synced %d model manifest(s)\n", resp.Synced)
	return nil
}

func invite(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("invite command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "create":
		return inviteCreate(args[1:])
	default:
		return fmt.Errorf("unknown invite command %q\n\n%s", args[0], usageText())
	}
}

func inviteCreate(args []string) error {
	fs := flag.NewFlagSet("invite create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fleetID := fs.String("fleet", "", "fleet id scoped to the invite")
	coordinatorURL := fs.String("coordinator", firstNonEmpty(os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "http://127.0.0.1:8080"), "coordinator base URL")
	operatorToken := fs.String("operator-token", os.Getenv("THIRDSHIFT_OPERATOR_TOKEN"), "operator bearer token")
	expiresIn := fs.Duration("expires-in", 24*time.Hour, "invite expiry duration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fleetID == "" {
		return fmt.Errorf("--fleet is required")
	}
	if *operatorToken == "" {
		return fmt.Errorf("operator token is required; set THIRDSHIFT_OPERATOR_TOKEN or pass --operator-token")
	}
	var resp struct {
		InviteID  string    `json:"invite_id"`
		FleetID   string    `json:"fleet_id"`
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	err := postAdminJSON(*coordinatorURL+"/internal/v1/invites", *operatorToken, map[string]any{
		"fleet_id":           *fleetID,
		"expires_in_seconds": int64(expiresIn.Seconds()),
	}, &resp)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "invite_id: %s\nfleet_id: %s\ntoken: %s\nexpires_at: %s\n", resp.InviteID, resp.FleetID, resp.Token, resp.ExpiresAt.Format(time.RFC3339))
	return nil
}

func fleet(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("fleet command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "create":
		return fleetCreate(args[1:])
	case "report":
		return fleetReport(args[1:])
	default:
		return fmt.Errorf("unknown fleet command %q\n\n%s", args[0], usageText())
	}
}

func fleetCreate(args []string) error {
	fs := flag.NewFlagSet("fleet create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	orgID := fs.String("org", "", "organization id")
	name := fs.String("name", "", "fleet name")
	scheduleFrom := fs.String("schedule-from", "", "optional fleet default local schedule start in HH:MM")
	scheduleUntil := fs.String("schedule-until", "", "optional fleet default local schedule end in HH:MM")
	coordinatorURL := fs.String("coordinator", firstNonEmpty(os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "http://127.0.0.1:8080"), "coordinator base URL")
	operatorToken := fs.String("operator-token", os.Getenv("THIRDSHIFT_OPERATOR_TOKEN"), "operator bearer token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *orgID == "" {
		return fmt.Errorf("--org is required")
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}
	if *operatorToken == "" {
		return fmt.Errorf("operator token is required; set THIRDSHIFT_OPERATOR_TOKEN or pass --operator-token")
	}
	var resp struct {
		ID             string `json:"id"`
		OrganizationID string `json:"organization_id"`
		Name           string `json:"name"`
		ScheduleFrom   string `json:"schedule_from"`
		ScheduleUntil  string `json:"schedule_until"`
	}
	err := postAdminJSON(*coordinatorURL+"/internal/v1/fleets", *operatorToken, map[string]string{
		"org_id":         *orgID,
		"name":           *name,
		"schedule_from":  *scheduleFrom,
		"schedule_until": *scheduleUntil,
	}, &resp)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "fleet_id: %s\norg_id: %s\nname: %s\n", resp.ID, resp.OrganizationID, resp.Name)
	if resp.ScheduleFrom != "" || resp.ScheduleUntil != "" {
		fmt.Fprintf(os.Stdout, "schedule: %s-%s\n", resp.ScheduleFrom, resp.ScheduleUntil)
	}
	return nil
}

func fleetReport(args []string) error {
	fs := flag.NewFlagSet("fleet report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fleetID := fs.String("fleet", "", "fleet id")
	fromRaw := fs.String("from", "", "inclusive RFC3339 timestamp or YYYY-MM-DD date")
	untilRaw := fs.String("to", "", "exclusive RFC3339 timestamp or YYYY-MM-DD date")
	outPath := fs.String("out", "", "optional output CSV path; stdout when omitted")
	coordinatorURL := fs.String("coordinator", firstNonEmpty(os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "http://127.0.0.1:8080"), "coordinator base URL")
	operatorToken := fs.String("operator-token", os.Getenv("THIRDSHIFT_OPERATOR_TOKEN"), "operator bearer token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fleetID == "" {
		return fmt.Errorf("--fleet is required")
	}
	if *operatorToken == "" {
		return fmt.Errorf("operator token is required; set THIRDSHIFT_OPERATOR_TOKEN or pass --operator-token")
	}
	endpoint := strings.TrimRight(*coordinatorURL, "/") + "/internal/v1/fleets/" + url.PathEscape(*fleetID) + "/report"
	query := url.Values{}
	if *fromRaw != "" {
		query.Set("from", *fromRaw)
	}
	if *untilRaw != "" {
		query.Set("to", *untilRaw)
	}
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	body, err := getAdminRaw(endpoint, *operatorToken)
	if err != nil {
		return err
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, body, 0o600); err != nil {
			return fmt.Errorf("write fleet report CSV: %w", err)
		}
		fmt.Fprintf(os.Stdout, "fleet_report: %s\n", *outPath)
		return nil
	}
	_, err = os.Stdout.Write(body)
	return err
}

func nodes(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("nodes command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "list":
		return nodesList(args[1:])
	default:
		return fmt.Errorf("unknown nodes command %q\n\n%s", args[0], usageText())
	}
}

func nodesList(args []string) error {
	fs := flag.NewFlagSet("nodes list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	coordinatorURL := fs.String("coordinator", firstNonEmpty(os.Getenv("THIRDSHIFT_COORDINATOR_URL"), "http://127.0.0.1:8080"), "coordinator base URL")
	operatorToken := fs.String("operator-token", os.Getenv("THIRDSHIFT_OPERATOR_TOKEN"), "operator bearer token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *operatorToken == "" {
		return fmt.Errorf("operator token is required; set THIRDSHIFT_OPERATOR_TOKEN or pass --operator-token")
	}
	var resp struct {
		Nodes []struct {
			ID              string     `json:"id"`
			State           string     `json:"state"`
			CurrentModelID  string     `json:"current_model_id"`
			LastSeenAt      *time.Time `json:"last_seen_at"`
			SessionStatus   string     `json:"session_status"`
			LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
			ScheduleState   string     `json:"schedule_state"`
			ThermalState    string     `json:"thermal_state"`
			Paused          bool       `json:"paused"`
			Draining        bool       `json:"draining"`
		} `json:"nodes"`
	}
	if err := getAdminJSON(*coordinatorURL+"/internal/v1/nodes", *operatorToken, &resp); err != nil {
		return err
	}
	if len(resp.Nodes) == 0 {
		fmt.Fprintln(os.Stdout, "no nodes")
		return nil
	}
	fmt.Fprintln(os.Stdout, "NODE_ID\tSTATE\tSESSION\tLAST_HEARTBEAT_AGE\tMODEL\tSCHEDULE\tTHERMAL\tPAUSED\tDRAINING")
	now := time.Now().UTC()
	for _, node := range resp.Nodes {
		age := "never"
		if node.LastHeartbeatAt != nil {
			age = now.Sub(*node.LastHeartbeatAt).Round(time.Second).String()
		}
		model := node.CurrentModelID
		if model == "" {
			model = "-"
		}
		schedule := node.ScheduleState
		if schedule == "" {
			schedule = "-"
		}
		thermal := node.ThermalState
		if thermal == "" {
			thermal = "-"
		}
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%t\n", node.ID, node.State, node.SessionStatus, age, model, schedule, thermal, node.Paused, node.Draining)
	}
	return nil
}

func credits(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("credits command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "release":
		return creditsRelease(args[1:])
	default:
		return fmt.Errorf("unknown credits command %q\n\n%s", args[0], usageText())
	}
}

func creditsRelease(args []string) error {
	fs := flag.NewFlagSet("credits release", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, store, err := ledgerStore(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	released, err := store.PromoteAvailableCredits(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "credits_released: %d\n", released)
	return nil
}

func payout(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("payout command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "create":
		return payoutCreate(args[1:])
	case "export":
		return payoutExport(args[1:])
	case "confirm":
		return payoutConfirm(args[1:])
	case "void":
		return payoutVoid(args[1:])
	default:
		return fmt.Errorf("unknown payout command %q\n\n%s", args[0], usageText())
	}
}

func payoutCreate(args []string) error {
	fs := flag.NewFlagSet("payout create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	orgID := fs.String("org", "", "optional organization id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, store, err := ledgerStore(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	batch, err := store.CreatePayoutBatch(ctx, *orgID, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "batch_id: %s\nstatus: %s\ntotal_microdollars: %d\nitem_count: %d\n", batch.ID, batch.Status, batch.TotalMicrodollars, batch.ItemCount)
	return nil
}

func payoutExport(args []string) error {
	fs := flag.NewFlagSet("payout export", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	batchID := fs.String("batch", "", "payout batch id")
	outPath := fs.String("out", "", "optional output CSV path; stdout when omitted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *batchID == "" {
		return fmt.Errorf("--batch is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, store, err := ledgerStore(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	body, batch, err := store.ExportPayoutBatch(ctx, *batchID, time.Now().UTC())
	if err != nil {
		return err
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, body, 0o600); err != nil {
			return fmt.Errorf("write payout CSV: %w", err)
		}
		fmt.Fprintf(os.Stdout, "batch_id: %s\nstatus: %s\ncsv_sha256: %s\n", batch.ID, batch.Status, batch.ExportedCSVChecksum)
		return nil
	}
	_, err = os.Stdout.Write(body)
	return err
}

func payoutConfirm(args []string) error {
	fs := flag.NewFlagSet("payout confirm", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	batchID := fs.String("batch", "", "payout batch id")
	filePath := fs.String("file", "", "paid confirmation CSV")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *batchID == "" {
		return fmt.Errorf("--batch is required")
	}
	if *filePath == "" {
		return fmt.Errorf("--file is required")
	}
	file, err := os.Open(*filePath)
	if err != nil {
		return fmt.Errorf("open confirmation CSV: %w", err)
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, store, err := ledgerStore(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	batch, err := store.ConfirmPayoutBatch(ctx, *batchID, file, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "batch_id: %s\nstatus: %s\ntransaction_id: %s\n", batch.ID, batch.Status, batch.TransactionID)
	return nil
}

func payoutVoid(args []string) error {
	fs := flag.NewFlagSet("payout void", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	batchID := fs.String("batch", "", "payout batch id")
	reason := fs.String("reason", "void payout batch", "void reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *batchID == "" {
		return fmt.Errorf("--batch is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, store, err := ledgerStore(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	batch, err := store.VoidPayoutBatch(ctx, *batchID, *reason, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "batch_id: %s\nstatus: %s\n", batch.ID, batch.Status)
	if batch.TransactionID != "" {
		fmt.Fprintf(os.Stdout, "reversal_transaction_id: %s\n", batch.TransactionID)
	}
	return nil
}

func report(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("report command is required\n\n%s", usageText())
	}
	switch args[0] {
	case "economics":
		return reportEconomics(args[1:])
	default:
		return fmt.Errorf("unknown report command %q\n\n%s", args[0], usageText())
	}
}

func reportEconomics(args []string) error {
	fs := flag.NewFlagSet("report economics", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	fromRaw := fs.String("from", "", "inclusive RFC3339 timestamp")
	untilRaw := fs.String("until", "", "exclusive RFC3339 timestamp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	from, err := parseOptionalTime(*fromRaw)
	if err != nil {
		return err
	}
	until, err := parseOptionalTime(*untilRaw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, store, err := ledgerStore(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	report, err := store.EconomicsReport(ctx, from, until)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "customer_revenue_microdollars: %d\nhost_credits_microdollars: %d\nverification_overhead_microdollars: %d\nfailed_attempt_overhead_microdollars: %d\ncontribution_margin_microdollars: %d\n",
		report.CustomerRevenueMicrodollars,
		report.HostCreditsMicrodollars,
		report.VerificationOverheadMicrodollars,
		report.FailedAttemptOverheadMicrodollars,
		report.ContributionMarginMicrodollars)
	return nil
}

func migrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	migrationsDir := fs.String("migrations-dir", "migrations", "directory containing *.up.sql migrations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *databaseURL == "" {
		return fmt.Errorf("database URL is required; set THIRDSHIFT_DATABASE_URL or DATABASE_URL, or pass --database-url")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applied, err := database.ApplyURL(ctx, *databaseURL, *migrationsDir)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Fprintln(os.Stdout, "migrations: no changes")
		return nil
	}
	for _, migration := range applied {
		fmt.Fprintf(os.Stdout, "migrations: applied %s %s\n", migration.Version, migration.Name)
	}
	return nil
}

func usage() error {
	fmt.Fprint(os.Stderr, usageText())
	return nil
}

func usageText() string {
	return "admin-cli commands:\n  migrate [--database-url URL] [--migrations-dir migrations]\n  org create --name <name> [--coordinator URL]\n  catalog sync [--catalog-dir models/catalog] [--coordinator URL]\n  apikey create --org <org_id> [--model <model_id>] [--coordinator URL]\n  invite create --fleet <fleet_id> [--coordinator URL]\n  fleet create --org <org_id> --name <name> [--schedule-from HH:MM --schedule-until HH:MM] [--coordinator URL]\n  fleet report --fleet <fleet_id> [--from RFC3339] [--to RFC3339] [--out report.csv] [--coordinator URL]\n  nodes list [--coordinator URL]\n  credits release [--database-url URL]\n  payout create [--org <org_id>] [--database-url URL]\n  payout export --batch <batch_id> [--out paid.csv] [--database-url URL]\n  payout confirm --batch <batch_id> --file paid.csv [--database-url URL]\n  payout void --batch <batch_id> [--reason text] [--database-url URL]\n  report economics [--from RFC3339] [--until RFC3339] [--database-url URL]\n"
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	if value != "" {
		*m = append(*m, value)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ledgerStore(ctx context.Context, databaseURL string) (*pgxpool.Pool, ledger.Store, error) {
	if databaseURL == "" {
		return nil, ledger.Store{}, fmt.Errorf("database URL is required; set THIRDSHIFT_DATABASE_URL or DATABASE_URL, or pass --database-url")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, ledger.Store{}, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, ledger.Store{}, fmt.Errorf("ping database: %w", err)
	}
	return pool, ledger.Store{Pool: pool}, nil
}

func parseOptionalTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q as RFC3339: %w", value, err)
	}
	return parsed.UTC(), nil
}

func postAdminJSON(endpoint, token string, body any, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func getAdminJSON(endpoint, token string, target any) error {
	body, err := getAdminRaw(endpoint, token)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func getAdminRaw(endpoint, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeAPIError(resp)
	}
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

func decodeAPIError(resp *http.Response) error {
	var body struct {
		Error any `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	message := resp.Status
	switch errBody := body.Error.(type) {
	case string:
		if errBody != "" {
			message = errBody
		}
	case map[string]any:
		if msg, ok := errBody["message"].(string); ok && msg != "" {
			message = msg
		}
	}
	return fmt.Errorf("%s: %s", resp.Request.URL.String(), strings.TrimSpace(message))
}
