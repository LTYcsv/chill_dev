package main

import (
	"os/exec"
	"strings"
)

func detectDocker() string {
	cmd := exec.Command("docker", "info", "--format", "{{.ServerVersion}}")
	out, err := cmd.Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(out))
}
