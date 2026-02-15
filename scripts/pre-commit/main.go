package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	errors int
	green  = "\033[32m"
	red    = "\033[31m"
	bold   = "\033[1m"
	reset  = "\033[0m"
)

func pass(msg string) {
	fmt.Printf("  %s ok%s: %s\n", green, reset, msg)
}

func fail(msg string) {
	fmt.Printf("  %sFAIL%s: %s\n", red, reset, msg)
	errors++
}

func section(name string) {
	fmt.Printf("\n%s=== %s ===%s\n", bold, name, reset)
}

// repoRoot walks up from cwd to find the .git directory.
func repoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: not a git repository\n")
		os.Exit(1)
	}
	return strings.TrimSpace(string(out))
}

// checkJSON validates all .json files under dashboards/ are parseable JSON.
func checkJSON(root string) {
	section("JSON validation")

	matches, _ := filepath.Glob(filepath.Join(root, "dashboards", "*.json"))
	for _, f := range matches {
		info, err := os.Stat(f)
		if err != nil || info.IsDir() {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			fail(fmt.Sprintf("%s — cannot read: %v", rel(root, f), err))
			continue
		}

		if !json.Valid(data) {
			fail(fmt.Sprintf("%s — invalid JSON", rel(root, f)))
		} else {
			pass(rel(root, f))
		}
	}
}

// checkYAML validates all known YAML config files parse correctly.
func checkYAML(root string) {
	section("YAML validation")

	yamlFiles := []string{
		"docker-compose.yml",
		"dashboards/claude-code-dashboards.yaml",
		"otelcol-config.yaml",
		"grafana/datasources.yaml",
		"prometheus/prometheus.yml",
		"loki/loki-config.yaml",
		"tempo/tempo-config.yaml",
		"pyroscope/pyroscope-config.yaml",
	}

	for _, name := range yamlFiles {
		f := filepath.Join(root, name)
		if _, err := os.Stat(f); os.IsNotExist(err) {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			fail(fmt.Sprintf("%s — cannot read: %v", name, err))
			continue
		}

		var out any
		if err := yaml.Unmarshal(data, &out); err != nil {
			fail(fmt.Sprintf("%s — invalid YAML: %v", name, err))
		} else {
			pass(name)
		}
	}
}

// checkDockerCompose runs `docker compose config --quiet`.
func checkDockerCompose(root string) {
	section("Docker Compose validation")

	cmd := exec.Command("docker", "compose", "config", "--quiet")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		fail(fmt.Sprintf("docker-compose.yml — %s", strings.TrimSpace(string(out))))
	} else {
		pass("docker-compose.yml")
	}
}

// floatingTags are image tags that indicate unpinned versions.
var floatingTags = map[string]bool{
	"latest":   true,
	"next":     true,
	"previous": true,
	"stable":   true,
	"edge":     true,
	"nightly":  true,
	"mainline": true,
	"lts":      true,
}

// checkVersionPinning validates all Docker image tags are pinned.
func checkVersionPinning(root string) {
	section("Version pinning")

	// Docker images in docker-compose.yml
	compose := filepath.Join(root, "docker-compose.yml")
	data, err := os.ReadFile(compose)
	if err != nil {
		fail(fmt.Sprintf("docker-compose.yml — cannot read: %v", err))
		return
	}

	imageRe := regexp.MustCompile(`(?m)^\s*image:\s*["']?([^\s"']+)["']?`)
	for _, match := range imageRe.FindAllStringSubmatch(string(data), -1) {
		image := match[1]
		parts := strings.SplitN(image, ":", 2)

		if len(parts) < 2 || parts[1] == "" {
			fail(fmt.Sprintf("docker-compose.yml — unpinned image (no tag): %s", image))
			continue
		}

		tag := parts[1]
		if floatingTags[tag] {
			fail(fmt.Sprintf("docker-compose.yml — floating tag '%s': %s", tag, image))
		} else {
			pass(fmt.Sprintf("docker-compose.yml — %s", image))
		}
	}

	// Go module pseudo-versions in hooks/go.mod (direct deps only)
	gomod := filepath.Join(root, "hooks", "go.mod")
	if _, err := os.Stat(gomod); os.IsNotExist(err) {
		return
	}

	gomodData, err := os.ReadFile(gomod)
	if err != nil {
		fail(fmt.Sprintf("hooks/go.mod — cannot read: %v", err))
		return
	}

	pseudoRe := regexp.MustCompile(`v0\.0\.0-\d{14}-[0-9a-f]+`)
	inIndirect := false
	for _, line := range strings.Split(string(gomodData), "\n") {
		trimmed := strings.TrimSpace(line)

		// Track whether we're inside an indirect require block
		if strings.Contains(trimmed, "// indirect") {
			continue
		}
		if trimmed == ")" {
			inIndirect = false
			continue
		}
		if inIndirect {
			continue
		}

		if pseudoRe.MatchString(trimmed) {
			dep := strings.Fields(trimmed)
			if len(dep) >= 2 {
				fail(fmt.Sprintf("hooks/go.mod — pseudo-version (unpinned): %s %s", dep[0], dep[1]))
			}
		}
	}

	if errors == 0 {
		pass("hooks/go.mod — all direct deps pinned")
	}
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

func main() {
	root := repoRoot()

	checkJSON(root)
	checkYAML(root)
	checkDockerCompose(root)
	checkVersionPinning(root)

	fmt.Println()
	if errors > 0 {
		fmt.Printf("%sPre-commit checks FAILED (%d error(s))%s\n", red, errors, reset)
		os.Exit(1)
	}
	fmt.Printf("%sPre-commit checks PASSED%s\n", green, reset)
}
