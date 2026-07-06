// gormtables scans one or more GORM model packages and generates an ordered
// Go source file that declares the AutoMigrate table list.
//
// Usage:
//
//	gormtables -models ./internal/model -out ./internal/db/tables.go
//
// See README.md for full flag documentation and examples.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gormtables:", err)
		flag.Usage()
		os.Exit(1)
	}

	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "gormtables:", err)
		os.Exit(1)
	}
}

// config holds all runtime parameters, parsed from CLI flags.
type config struct {
	modelDirs     []string // directories to scan for GORM model structs
	outFile       string   // path of the generated Go file
	outputPkg     string   // package name written in the generated file
	varName       string   // name of the generated slice variable
	baseEmbeds    []string // anonymous-embed names that trigger inclusion
	enableMarker  string   // comment marker to force-include a struct
	disableMarker string   // comment marker to force-exclude a struct
	tagKey        string   // struct-tag key for GORM foreign-key inspection
}

// parseFlags reads os.Args and returns a config, or an error if required
// flags are missing.
func parseFlags() (config, error) {
	models := flag.String("models", "", "comma-separated list of model directories to scan (required)")
	out := flag.String("out", "", "output file path (required)")
	pkg := flag.String("pkg", "database", "package name of the generated file")
	varName := flag.String("var", "autoMigrateTables", "name of the generated slice variable")
	base := flag.String("base", "BaseModel,gorm.Model", "comma-separated base embed names that qualify a struct for inclusion")
	enableMarker := flag.String("enable-marker", "AutoMigrate:enable", "comment marker to force-include a struct")
	disableMarker := flag.String("disable-marker", "AutoMigrate:disable", "comment marker to force-exclude a struct")
	tagKey := flag.String("tag", "gorm", "struct-tag key used for GORM foreign-key inspection")

	flag.Parse()

	if *models == "" {
		return config{}, fmt.Errorf("-models is required")
	}
	if *out == "" {
		return config{}, fmt.Errorf("-out is required")
	}

	dirs := splitTrimmed(*models, ",")
	bases := splitTrimmed(*base, ",")

	return config{
		modelDirs:     dirs,
		outFile:       *out,
		outputPkg:     *pkg,
		varName:       *varName,
		baseEmbeds:    bases,
		enableMarker:  *enableMarker,
		disableMarker: *disableMarker,
		tagKey:        *tagKey,
	}, nil
}

// splitTrimmed splits s by sep and trims whitespace from each element,
// discarding empty strings.
func splitTrimmed(s, sep string) []string {
	var out []string
	for p := range strings.SplitSeq(s, sep) {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// run is the application entry point after flag parsing.
func run(cfg config) error {
	modulePath, moduleRoot, err := readGoMod(cfg.modelDirs[0])
	if err != nil {
		return fmt.Errorf("reading go.mod: %w", err)
	}

	baseSet := make(map[string]bool, len(cfg.baseEmbeds))
	for _, b := range cfg.baseEmbeds {
		baseSet[b] = true
	}

	scanCfg := scanConfig{
		baseEmbeds:    baseSet,
		enableMarker:  cfg.enableMarker,
		disableMarker: cfg.disableMarker,
		tagKey:        cfg.tagKey,
		outputPkg:     cfg.outputPkg,
		modulePath:    modulePath,
		moduleRoot:    moduleRoot,
	}

	models, imports, err := scanDirs(cfg.modelDirs, scanCfg)
	if err != nil {
		return fmt.Errorf("scanning models: %w", err)
	}

	sorted, err := topoSort(models)
	if err != nil {
		return fmt.Errorf("sorting models: %w", err)
	}

	src, err := renderOutput(templateData{
		PkgName: cfg.outputPkg,
		Imports: imports,
		VarName: cfg.varName,
		Models:  sorted,
	})
	if err != nil {
		return fmt.Errorf("rendering output: %w", err)
	}

	// Ensure parent directory of the output file exists.
	if dir := filepath.Dir(cfg.outFile); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	}

	if err := os.WriteFile(cfg.outFile, src, 0644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

// readGoMod walks up from startDir looking for a go.mod file and returns the
// module path declared inside it as well as the directory containing go.mod.
func readGoMod(startDir string) (modulePath, moduleRoot string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", err
	}

	for {
		candidate := filepath.Join(dir, "go.mod")
		if _, statErr := os.Stat(candidate); statErr == nil {
			path, readErr := parseModulePath(candidate)
			if readErr != nil {
				return "", "", readErr
			}
			return path, dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", "", fmt.Errorf("go.mod not found from %s upward", startDir)
}

// parseModulePath reads the module directive from a go.mod file.
func parseModulePath(goModPath string) (string, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after), nil
		}
	}
	return "", fmt.Errorf("module directive not found in %s", goModPath)
}
