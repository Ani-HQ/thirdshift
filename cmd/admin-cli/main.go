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
		} `json:"nodes"`
	}
	if err := getAdminJSON(*coordinatorURL+"/internal/v1/nodes", *operatorToken, &resp); err != nil {
		return err
	}
	if len(resp.Nodes) == 0 {
		fmt.Fprintln(os.Stdout, "no nodes")
		return nil
	}
	fmt.Fprintln(os.Stdout, "NODE_ID\tSTATE\tSESSION\tLAST_HEARTBEAT_AGE\tMODEL")
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
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%s\n", node.ID, node.State, node.SessionStatus, age, model)
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
	return "admin-cli commands:\n  migrate [--database-url URL] [--migrations-dir migrations]\n  invite create --fleet <fleet_id> [--coordinator URL]\n  nodes list [--coordinator URL]\n"
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
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Error == "" {
		body.Error = resp.Status
	}
	return fmt.Errorf("%s: %s", resp.Request.URL.String(), strings.TrimSpace(body.Error))
}
