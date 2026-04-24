package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/oaooao/capx/internal/config"
	capxserver "github.com/oaooao/capx/internal/server"
	"github.com/oaooao/capx/internal/setup"
	"gopkg.in/yaml.v3"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	configPath := resolveConfigPath()

	switch os.Args[1] {
	case "serve":
		cmdServe(configPath)
	case "list":
		cmdList(configPath)
	case "scenes":
		cmdScenes(configPath)
	case "scene":
		cmdScene(configPath)
	case "add":
		cmdAdd(configPath)
	case "setup":
		cmdSetup(configPath)
	case "init":
		cmdInit()
	case "dump":
		cmdDump()
	case "migrate":
		cmdMigrate()
	case "version":
		fmt.Printf("capx %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func resolveConfigPath() string {
	// --config flag
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	// CAPX_CONFIG env
	if p := os.Getenv("CAPX_CONFIG"); p != "" {
		return p
	}
	return config.DefaultConfigPath()
}

// configPathExplicit reports whether --config or CAPX_CONFIG was set.
// When true, commands should honor the legacy single-file path; otherwise
// they should use LoadMerged to walk v0.2 scope discovery.
func configPathExplicit() bool {
	for _, arg := range os.Args {
		if arg == "--config" || strings.HasPrefix(arg, "--config=") {
			return true
		}
	}
	return os.Getenv("CAPX_CONFIG") != ""
}

// loadConfig picks the right loader: legacy Load when the user pinned a
// specific v0.1 file via --config / CAPX_CONFIG, LoadMerged (scope discovery)
// otherwise. This keeps read-only commands consistent with `capx dump` and
// with the MCP server's runtime view.
func loadConfig(configPath string) (*config.Config, error) {
	if configPathExplicit() {
		return config.Load(configPath)
	}
	pwd, _ := os.Getwd()
	return config.LoadMerged(pwd)
}

func cmdServe(configPath string) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	scene := os.Getenv("CAPX_SCENE")
	if scene == "" {
		scene = cfg.DefaultScene
	}

	if err := capxserver.Serve(cfg, scene); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdList(configPath string) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	visible := cfg.VisibleCapabilities()
	for name, cap := range visible {
		tags := ""
		if len(cap.Tags) > 0 {
			tags = " [" + strings.Join(cap.Tags, ", ") + "]"
		}
		fmt.Printf("  %-30s %s — %s%s\n", name, cap.Type, cap.Description, tags)
	}
}

func cmdScenes(configPath string) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	for name := range cfg.Scenes {
		fmt.Println(name)
	}
}

func cmdScene(configPath string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: capx scene list")
		os.Exit(1)
	}
	if os.Args[2] != "list" {
		fmt.Fprintf(os.Stderr, "unknown scene subcommand: %s\n", os.Args[2])
		os.Exit(1)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for name, scene := range cfg.Scenes {
		marker := " "
		if name == cfg.DefaultScene {
			marker = "*"
		}
		fmt.Printf(" %s %-20s [%s]\n", marker, name, strings.Join(scene.AutoEnable.All(), ", "))
	}
}

func cmdAdd(configPath string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: capx add <name> --type mcp|cli [--transport stdio|http] [--command cmd] [--url url] [--description desc]")
		os.Exit(1)
	}

	name := os.Args[2]
	cap := &config.Capability{}

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--type":
			i++
			cap.Type = args[i]
		case "--transport":
			i++
			cap.Transport = args[i]
		case "--command":
			i++
			cap.Command = args[i]
		case "--url":
			i++
			cap.URL = args[i]
		case "--description":
			i++
			cap.Description = args[i]
		case "--args":
			i++
			// Parse JSON array or comma-separated.
			val := args[i]
			if strings.HasPrefix(val, "[") {
				var parsed []string
				json.Unmarshal([]byte(val), &parsed)
				cap.Args = parsed
			} else {
				cap.Args = strings.Split(val, ",")
			}
		}
	}

	if cap.Type == "" {
		fmt.Fprintln(os.Stderr, "error: --type is required")
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		cfg = &config.Config{
			Capabilities: make(map[string]*config.Capability),
			Scenes:       map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
			DefaultScene: "default",
		}
	}

	cfg.Capabilities[name] = cap
	if err := config.Save(cfg, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added capability %q to %s\n", name, configPath)
}

func cmdSetup(configPath string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: capx setup <claude-code|codex>")
		os.Exit(1)
	}

	var err error
	switch os.Args[2] {
	case "claude-code":
		err = setup.SetupClaudeCode(configPath)
	case "codex":
		err = setup.SetupCodex(configPath)
	default:
		fmt.Fprintf(os.Stderr, "unknown setup target: %s\n", os.Args[2])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdDump() {
	var (
		scene      string
		format     = "json"
		configDir  string
		schemaVer  = config.DumpSchemaVersion
	)
	for i := 2; i < len(os.Args); i++ {
		a := os.Args[i]
		switch {
		case a == "--scene" && i+1 < len(os.Args):
			scene = os.Args[i+1]
			i++
		case strings.HasPrefix(a, "--scene="):
			scene = strings.TrimPrefix(a, "--scene=")
		case a == "--format" && i+1 < len(os.Args):
			format = os.Args[i+1]
			i++
		case strings.HasPrefix(a, "--format="):
			format = strings.TrimPrefix(a, "--format=")
		case a == "--config" && i+1 < len(os.Args):
			configDir = os.Args[i+1]
			i++
		case strings.HasPrefix(a, "--config="):
			configDir = strings.TrimPrefix(a, "--config=")
		case a == "--schema-version" && i+1 < len(os.Args):
			v, err := strconv.Atoi(os.Args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "--schema-version expects integer, got %q\n", os.Args[i+1])
				os.Exit(1)
			}
			schemaVer = v
			i++
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", a)
			os.Exit(1)
		}
	}
	if schemaVer != config.DumpSchemaVersion {
		fmt.Fprintf(os.Stderr, "unsupported schema version %d (this capx supports %d)\n",
			schemaVer, config.DumpSchemaVersion)
		os.Exit(1)
	}

	pwd, _ := os.Getwd()
	if configDir != "" {
		// --config asks "show me what's in this directory only". That's
		// single-scope semantics, so we set BOTH CAPX_HOME (relocates
		// global) and CAPX_ISOLATE=1 (skips project discovery). Without
		// ISOLATE, an ambient .capx/ walking up from pwd would leak into
		// the dump and contradict the user's intent.
		os.Setenv("CAPX_HOME", configDir)
		os.Setenv("CAPX_ISOLATE", "1")
	}
	cfg, err := config.LoadMerged(pwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	dump, err := config.Dump(cfg, scene, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	switch format {
	case "json":
		payload, _ := json.MarshalIndent(dump, "", "  ")
		fmt.Println(string(payload))
	case "yaml":
		payload, err := yaml.Marshal(dump)
		if err != nil {
			fmt.Fprintf(os.Stderr, "yaml encode: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(string(payload))
	default:
		fmt.Fprintf(os.Stderr, "unsupported --format %q (json | yaml)\n", format)
		os.Exit(1)
	}
}

func cmdMigrate() {
	opts := setup.MigrateOptions{}
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--dry-run":
			opts.DryRun = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", os.Args[i])
			os.Exit(1)
		}
	}
	report, err := setup.Migrate(opts)
	if report != nil {
		payload, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(payload))
	}
	if err != nil {
		os.Exit(1)
	}
}

func cmdInit() {
	opts := setup.InitOptions{}
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--global":
			opts.Global = true
		case "--add-scenes":
			opts.AddScenes = true
		case "--force":
			opts.Force = true
		case "--agent":
			if i+1 >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "--agent requires a value (claude-code | codex)")
				os.Exit(1)
			}
			opts.Agent = os.Args[i+1]
			i++
		default:
			if strings.HasPrefix(os.Args[i], "--agent=") {
				opts.Agent = strings.TrimPrefix(os.Args[i], "--agent=")
			} else {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n", os.Args[i])
				os.Exit(1)
			}
		}
	}
	if err := setup.Init(opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`capx — Agent Capability Runtime

Usage:
  capx serve                           Start MCP server (stdio)
  capx list                            List all configured capabilities
  capx scene list                      List all scenes
  capx scenes                          Scene names, one per line (shell completion)
  capx add <name> [options]            Add a capability to config

  capx init [--global] [--add-scenes]  Initialize .capx/ scope
  capx init --agent <name>             Register capx with an agent (claude-code | codex)
  capx dump [--scene <n>] [--format json|yaml] [--config <dir>]
                                       Authoritative merged view (v1 schema)
  capx migrate [--dry-run]             Convert ~/.config/capx/config.yaml → v0.2 structure
  capx setup claude-code | codex       Legacy agent setup (superseded by init --agent)
  capx version                         Print version

Flags:
  --config <path>             v0.1 config file path (default: ~/.config/capx/config.yaml)

Environment:
  CAPX_HOME                   Replace global scope directory (~/.config/capx by default).
                              Project .capx/ discovery still applies on top.
  CAPX_ISOLATE                Set to 1 to force single-scope mode (skip project
                              discovery). Use with CAPX_HOME for tests, CI, or
                              diagnosing configs without project overrides.
  CAPX_CONFIG                 Legacy v0.1 config file path override
  CAPX_SCENE                  Scene to use at startup`)
}
