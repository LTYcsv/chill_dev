package main

// devp — Developer Infrastructure Platform CLI
// Usage: devp <command> [flags]
//
// devp deploy --service api --env production
// devp logs --service api --tail 100
// devp secrets set DATABASE_URL postgres://...
// devp graph --project myapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// ─── Config ───────────────────────────────────────────────────

type CLIConfig struct {
	GatewayURL string
	Token      string
}

func loadCLIConfig() CLIConfig {
	return CLIConfig{
		GatewayURL: getEnv("DEVP_API_URL", "http://localhost:8080"),
		Token:      getEnv("DEVP_TOKEN", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── HTTP Client ──────────────────────────────────────────────

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(cfg CLIConfig) *Client {
	return &Client{
		baseURL: cfg.GatewayURL,
		token:   cfg.Token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.http.Do(req)
}

func (c *Client) JSON(method, path string, body any, out any) error {
	resp, err := c.do(method, path, body)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, errResp["error"])
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// ─── Commands ─────────────────────────────────────────────────

func cmdDeploy(client *Client, args []string) {
	serviceID := flagString(args, "--service", "")
	projectID := flagString(args, "--project", "")
	env := flagString(args, "--env", "production")
	branch := flagString(args, "--branch", "main")
	gitRepo := flagString(args, "--repo", "")

	if serviceID == "" {
		fatal("--service is required")
	}

	payload := map[string]string{
		"service_id":   serviceID,
		"project_id":   projectID,
		"git_repo":     gitRepo,
		"git_branch":   branch,
		"environment":  env,
		"triggered_by": "cli",
	}

	var result map[string]interface{}
	if err := client.JSON("POST", "/api/v1/deployments", payload, &result); err != nil {
		fatal("deploy failed: " + err.Error())
	}

	fmt.Printf("✓ Deployment triggered\n")
	fmt.Printf("  ID:          %s\n", result["id"])
	fmt.Printf("  Service:     %s\n", result["service_id"])
	fmt.Printf("  Environment: %s\n", result["environment"])
	fmt.Printf("  Status:      %s\n", result["status"])
	fmt.Printf("\nTrack: devp status --deployment %s\n", result["id"])
}

func cmdStatus(client *Client, args []string) {
	deploymentID := flagString(args, "--deployment", "")
	if deploymentID == "" {
		fatal("--deployment is required")
	}

	var result map[string]interface{}
	if err := client.JSON("GET", "/api/v1/deployments/"+deploymentID, nil, &result); err != nil {
		fatal(err.Error())
	}

	status := result["status"].(string)
	icon := statusIcon(status)

	fmt.Printf("%s Deployment %s\n", icon, deploymentID)
	fmt.Printf("   Status:  %s\n", status)
	fmt.Printf("   Service: %s\n", result["service_id"])
	if commit, ok := result["git_commit"].(string); ok && commit != "" {
		fmt.Printf("   Commit:  %s\n", commit[:8])
	}

	if logs, ok := result["logs"].([]interface{}); ok && len(logs) > 0 {
		fmt.Println("\nRecent logs:")
		for _, l := range logs {
			fmt.Println("  ", l)
		}
	}
}

func cmdGraph(client *Client, args []string) {
	projectID := flagString(args, "--project", "demo")
	env := flagString(args, "--env", "production")
	blastNode := flagString(args, "--blast-radius", "")

	if blastNode != "" {
		var result map[string]interface{}
		if err := client.JSON("GET", "/api/v1/graph/blast-radius/"+blastNode, nil, &result); err != nil {
			fatal(err.Error())
		}
		fmt.Printf("💥 Blast Radius for node: %s (%s)\n", result["node_name"], result["node_id"])
		fmt.Printf("   Severity: %s\n", strings.ToUpper(result["severity"].(string)))
		if names, ok := result["affected_names"].([]interface{}); ok {
			fmt.Printf("   Affected services (%d):\n", len(names))
			for _, n := range names {
				fmt.Printf("     • %s\n", n)
			}
		}
		return
	}

	var graph map[string]interface{}
	if err := client.JSON("GET", fmt.Sprintf("/api/v1/graph?project_id=%s&env=%s", projectID, env), nil, &graph); err != nil {
		fatal(err.Error())
	}

	nodes, _ := graph["nodes"].([]interface{})
	edges, _ := graph["edges"].([]interface{})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "NODE\tTYPE\tSTATUS\n")
	fmt.Fprintf(w, "────\t────\t──────\n")
	for _, n := range nodes {
		node := n.(map[string]interface{})
		icon := nodeIcon(node["type"].(string))
		fmt.Fprintf(w, "%s %s\t%s\t%s\n",
			icon, node["name"], node["type"], node["status"])
	}
	w.Flush()

	fmt.Printf("\nConnections (%d):\n", len(edges))
	for _, e := range edges {
		edge := e.(map[string]interface{})
		fmt.Printf("  %s → %s [%s] %v rps\n",
			edge["from"], edge["to"], edge["protocol"], edge["rps"])
	}
}

func cmdSecrets(client *Client, args []string) {
	if len(args) == 0 {
		fatal("usage: devp secrets [set|list|delete]")
	}
	sub := args[0]
	rest := args[1:]

	serviceID := flagString(rest, "--service", "")
	envID := flagString(rest, "--env-id", "")

	switch sub {
	case "set":
		if len(rest) < 2 {
			fatal("usage: devp secrets set KEY VALUE --service <id>")
		}
		key, value := rest[0], rest[1]
		payload := map[string]string{
			"service_id":     serviceID,
			"environment_id": envID,
			"key":            key,
			"value":          value,
		}
		var result map[string]interface{}
		if err := client.JSON("PUT", "/api/v1/secrets", payload, &result); err != nil {
			fatal(err.Error())
		}
		fmt.Printf("✓ Secret '%s' saved (id: %s)\n", key, result["id"])

	case "list":
		var result []map[string]interface{}
		url := fmt.Sprintf("/api/v1/secrets?service_id=%s&env_id=%s", serviceID, envID)
		if err := client.JSON("GET", url, nil, &result); err != nil {
			fatal(err.Error())
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "KEY\tID\tUPDATED\n")
		for _, s := range result {
			fmt.Fprintf(w, "%s\t%s\t%s\n", s["key"], s["id"], s["updated_at"])
		}
		w.Flush()

	default:
		fatal("unknown secrets subcommand: " + sub)
	}
}

func cmdLogin(client *Client, args []string) {
	email := flagString(args, "--email", "")
	password := flagString(args, "--password", "")
	if email == "" || password == "" {
		fatal("--email and --password are required")
	}
	var result map[string]string
	if err := client.JSON("POST", "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, &result); err != nil {
		fatal("login failed: " + err.Error())
	}
	fmt.Println("✓ Logged in successfully")
	fmt.Printf("\nSet your token:\n  export DEVP_TOKEN=%s\n", result["token"])
}

// ─── Helpers ──────────────────────────────────────────────────

func flagString(args []string, flag, defaultVal string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return defaultVal
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "Error: "+msg)
	os.Exit(1)
}

func statusIcon(status string) string {
	switch status {
	case "success":
		return "✓"
	case "failed":
		return "✗"
	case "building", "deploying":
		return "⟳"
	default:
		return "·"
	}
}

func nodeIcon(nodeType string) string {
	switch nodeType {
	case "service":
		return "⬡"
	case "database":
		return "⌥"
	case "queue":
		return "⇄"
	case "cache":
		return "⚡"
	default:
		return "○"
	}
}

// ─── Main ─────────────────────────────────────────────────────

func main() {
	cfg := loadCLIConfig()
	client := NewClient(cfg)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "deploy":
		cmdDeploy(client, args)
	case "status":
		cmdStatus(client, args)
	case "graph":
		cmdGraph(client, args)
	case "secrets":
		cmdSecrets(client, args)
	case "login":
		cmdLogin(client, args)
	case "health":
		var result map[string]interface{}
		if err := client.JSON("GET", "/healthz", nil, &result); err != nil {
			fatal(err.Error())
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`devp — Developer Infrastructure Platform CLI

Usage: devp <command> [options]

Commands:
  login      Authenticate with the platform
  deploy     Trigger a deployment
  status     Check deployment status
  graph      View infrastructure graph
  secrets    Manage environment secrets
  health     Check platform health

Examples:
  devp login --email you@company.com --password secret
  devp deploy --service api --project myapp --env production --repo https://github.com/me/api
  devp status --deployment abc123
  devp graph --project myapp --env production
  devp graph --blast-radius postgres-main
  devp secrets set DATABASE_URL postgres://... --service api --env-id prod-env-id
  devp secrets list --service api --env-id prod-env-id

Environment variables:
  DEVP_API_URL   Platform API URL (default: http://localhost:8080)
  DEVP_TOKEN     Authentication token (from 'devp login')
`)
}
