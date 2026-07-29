package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/database"
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
	case "nodes":
		return nodes(args[1:])
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
	return "admin-cli commands:\n  migrate [--database-url URL] [--migrations-dir migrations]\n  org create --name <name> [--coordinator URL]\n  catalog sync [--catalog-dir models/catalog] [--coordinator URL]\n  apikey create --org <org_id> [--model <model_id>] [--coordinator URL]\n  invite create --fleet <fleet_id> [--coordinator URL]\n  nodes list [--coordinator URL]\n"
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
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
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
