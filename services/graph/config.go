package main

import (
	"encoding/json"
	"os"
)

type AppConfig struct {
	ConfigPath  string
	Port        string
	DatabaseURL string
	NATSURL     string
}

func loadAppConfig() AppConfig {
	return AppConfig{
		ConfigPath:  getEnv("GRAPH_CONFIG", "./graph.json"),
		Port:        getEnv("PORT", "8087"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		NATSURL:     getEnv("NATS_URL", "nats://localhost:4222"),
	}
}

func loadConfig(path string) (map[string]*GraphNode, map[string]*GraphEdge, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var cfg GraphConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, nil, err
	}

	nodes := make(map[string]*GraphNode, len(cfg.Nodes))
	edges := make(map[string]*GraphEdge, len(cfg.Edges))
	for i := range cfg.Nodes {
		n := cfg.Nodes[i]
		nodes[n.ID] = &n
	}
	for i := range cfg.Edges {
		e := cfg.Edges[i]
		edges[e.ID] = &e
	}
	return nodes, edges, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
