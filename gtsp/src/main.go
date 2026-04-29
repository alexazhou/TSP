package main

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// PathRuleList is a flag.Value that converts comma-separated paths into allow PathRules
type PathRuleList struct {
	Rules []api.PathRule
}

func (p *PathRuleList) String() string {
	var paths []string
	for _, r := range p.Rules {
		paths = append(paths, r.Path)
	}
	return strings.Join(paths, ",")
}

func (p *PathRuleList) Set(s string) error {
	if s == "" || s == "true" {
		return nil
	}
	for _, raw := range strings.Split(s, ",") {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		p.Rules = append(p.Rules, api.PathRule{Action: "allow", Path: path})
	}
	return nil
}

func (p *PathRuleList) IsBoolFlag() bool { return true }

func printUsage() {
	fmt.Printf("gTSP %s\n\n", api.Version)
	fmt.Println("Usage: gtsp [options] [command]")
	fmt.Println("\nCommands:")
	fmt.Println("  schema [-o file]       Output JSON schema for all tools")
	fmt.Println("\nOptions:")
	fmt.Println("  -h, --help             Show this help message")
	fmt.Println("  -v, --version          Show version")
	fmt.Println("  --mode string          Communication mode: 'stdio' or 'websocket' (default \"stdio\")")
	fmt.Println("  --port int             WebSocket server port (required for websocket mode)")
	fmt.Println("  --sandbox              Enable sandbox restrictions (disabled by default)")
	fmt.Println("  --workdir path         Set process working directory (default current dir)")
	fmt.Println("  --access-root path     Restrict accessible paths to this root (requires --sandbox)")
	fmt.Println("  --allow-read=paths     Allow read tools restricted to comma-separated paths (requires --sandbox)")
	fmt.Println("  --allow-write=paths    Allow write tools restricted to comma-separated paths (requires --sandbox)")
	fmt.Println("  --log-path path        Directory to store log files (default ./logs)")
	fmt.Println()
}

func main() {
	// Determine default log path relative to executable
	exePath, _ := os.Executable()
	defaultLogPath := filepath.Join(filepath.Dir(exePath), "logs")
	
	// Default workdir root is current working directory
	cwd, _ := os.Getwd()

	// 1. Global Flags
	var help, version, schemaFlag bool
	flag.BoolVar(&help, "h", false, "Show this help message")
	flag.BoolVar(&help, "help", false, "Show this help message")
	flag.BoolVar(&version, "v", false, "Show version")
	flag.BoolVar(&version, "version", false, "Show version")
	
	port := flag.Int("port", 0, "Start WebSocket server on specified port (used with --mode websocket)")
	mode := flag.String("mode", "stdio", "The communication mode: 'stdio' or 'websocket'")
	logPath := flag.String("log-path", defaultLogPath, "The path to store log files")
	sandbox := flag.Bool("sandbox", false, "Enable sandbox restrictions")
	workdir := flag.String("workdir", "", "Set process working directory (default: current dir)")
	accessRoot := flag.String("access-root", "", "Restrict accessible paths to this root (requires --sandbox)")
	// Legacy flag support
	flag.StringVar(accessRoot, "workspace", "", "Deprecated: use --access-root")

	var allowRead, allowWrite PathRuleList
	flag.Var(&allowRead, "allow-read", "Allow read tools; optionally restrict to comma-separated paths")
	flag.Var(&allowWrite, "allow-write", "Allow write tools; optionally restrict to comma-separated paths")
	
	flag.BoolVar(&schemaFlag, "schema", false, "Output JSON schema for all tools")
	
	flag.Usage = printUsage
	flag.Parse()

	if help {
		printUsage()
		os.Exit(0)
	}

	if version {
		fmt.Println(api.Version)
		os.Exit(0)
	}

	// 2. Initialize Core Systems
	if err := api.InitLogger(*logPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		os.Exit(1)
	}

	// workdir: change process cwd if specified
	if *workdir != "" {
		if err := os.Chdir(*workdir); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting workdir: %v\n", err)
			os.Exit(1)
		}
	}

	// Sandbox setup: only when --sandbox is enabled
	var readRules, writeRules []api.PathRule
	if *sandbox {
		api.SetSandboxEnabled(true)

		// access-root: defaults to workdir (or cwd if workdir not set)
		root := *accessRoot
		if root == "" {
			if *workdir != "" {
				root = *workdir
			} else {
				root = cwd
			}
		}
		if err := api.SetWorkdirRoot(root); err != nil {
			log.Printf("Error initializing access root: %v", err)
			os.Exit(1)
		}
		log.Printf("Sandbox enabled. Access root: %s", api.GetWorkdirRoot())

		// permissions: if no flags, default to allowing workdir root
		readRules = allowRead.Rules
		if len(readRules) == 0 {
			readRules = []api.PathRule{{Action: "allow", Path: api.GetWorkdirRoot()}}
		}
		writeRules = allowWrite.Rules
		if len(writeRules) == 0 {
			writeRules = []api.PathRule{{Action: "allow", Path: api.GetWorkdirRoot()}}
		}
	} else {
		log.Printf("Sandbox disabled. No path restrictions.")
	}

	// Create initial session for stdio or as a template for WS
	initialSession := api.NewSession()
	initialSession.SetPathRules(readRules, writeRules)
	initialSession.SetNetworkAllowed(true)

	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher)

	// 3. Command Handling
	args := flag.Args()
	if (len(args) > 0 && args[0] == "schema") || schemaFlag {
		outputFile := ""
		if len(args) > 0 && args[0] == "schema" {
			schemaCmd := flag.NewFlagSet("schema", flag.ExitOnError)
			schemaOutput := schemaCmd.String("o", "", "Output file for the schema")
			schemaCmd.Parse(args[1:])
			outputFile = *schemaOutput
		}
		handleSchemaCommand(dispatcher, outputFile)
		os.Exit(0)
	}

	// 4. Mode Selection
	switch *mode {
	case "websocket":
		if *port <= 0 {
			fmt.Fprintf(os.Stderr, "Error: --port must be specified for websocket mode\n")
			os.Exit(1)
		}
		dispatcher.ServeWS(*port)
	case "stdio":
		stdioClient := api.NewStdioClient(os.Stdin, os.Stdout)
		dispatcher.ServeStdio(initialSession, stdioClient)
	default:
		log.Printf("Error: unknown mode %q", *mode)
		os.Exit(1)
	}
}

func handleSchemaCommand(dispatcher *api.Dispatcher, outputFile string) {
	schemasMap := dispatcher.GetSchemas()
	schemas := make([]interface{}, 0, len(schemasMap))
	
	order := []string{"list_dir", "read_file", "write_file", "edit", "grep_search", "glob", "execute_bash", "process_output", "process_stop"}
	for _, name := range order {
		if s, ok := schemasMap[name]; ok {
			schemas = append(schemas, s)
		}
	}
	for name, s := range schemasMap {
		found := false
		for _, orderedName := range order {
			if name == orderedName {
				found = true
				break
			}
		}
		if !found {
			schemas = append(schemas, s)
		}
	}

	data, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		log.Printf("error marshaling schemas: %v", err)
		os.Exit(1)
	}

	if outputFile != "" {
		err := os.WriteFile(outputFile, data, 0644)
		if err != nil {
			log.Printf("error writing schema to file %s: %v", outputFile, err)
			os.Exit(1)
		}
		log.Printf("Schema successfully written to %s", outputFile)
	} else {
		fmt.Println(string(data))
	}
}
