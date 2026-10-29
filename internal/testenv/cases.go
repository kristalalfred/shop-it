package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kristalalfred/shop-it/internal/backend"
	"github.com/kristalalfred/shop-it/internal/mockio"
)

type Case struct {
	Name          string                  `json:"name"`
	File          string                  `json:"file"`
	Variables     map[string]string       `json:"variables,omitempty"`
	CatalogFaults map[string]mockio.Fault `json:"catalog_faults,omitempty"`
	Browser       *mockio.BrowserScenario `json:"browser,omitempty"`
	Seeds         []SeedTransfer          `json:"seeds,omitempty"`
}

func Run(ctx context.Context, selection string, output io.Writer) (err error) {
	cases, err := loadCases("tests/api/cases.json", selection)
	if err != nil {
		return err
	}
	includeGo := selection == "all" || selection == "concurrency"
	if len(cases) == 0 && !includeGo {
		return fmt.Errorf("no test cases match %q", selection)
	}
	if len(cases) > 0 {
		if _, err := exec.LookPath("hurl"); err != nil {
			return fmt.Errorf("hurl is required; install it with brew install hurl")
		}
	}
	dir, err := os.MkdirTemp("", "shop-it-test-suite-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	binary := filepath.Join(dir, "shop-it")
	fmt.Fprintln(output, "Building backend with race detection...")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/shop-it")
	if data, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build backend: %w\n%s", err, data)
	}
	failures, passed := 0, 0
	if includeGo {
		packages := "./..."
		if selection == "concurrency" {
			packages = "./tests"
		}
		cmd := exec.CommandContext(ctx, "go", "test", "-race", "-count=1", packages)
		cmd.Env = append(os.Environ(), "SHOP_IT_TEST_BINARY="+binary)
		if data, err := cmd.CombinedOutput(); err != nil {
			failures++
			fmt.Fprintf(output, "FAIL concurrency\n%s", data)
		} else {
			passed++
			fmt.Fprintln(output, "PASS concurrency")
		}
	}
	config, err := backend.LoadConfig("config/dev.json")
	if err != nil {
		return err
	}
	for _, tc := range cases {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runCase(ctx, binary, config, tc); err != nil {
			failures++
			fmt.Fprintf(output, "FAIL %s\n%v\n", tc.Name, err)
		} else {
			passed++
			fmt.Fprintf(output, "PASS %s\n", tc.Name)
		}
	}
	fmt.Fprintf(output, "\n%d passed, %d failed\n", passed, failures)
	if failures != 0 {
		return fmt.Errorf("test suite failed")
	}
	return nil
}

func loadCases(path, selection string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("cases file must contain one JSON array")
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("at least one API case is required")
	}
	names := map[string]bool{}
	references := map[string]bool{}
	selected := make([]Case, 0)
	for _, tc := range cases {
		if tc.Name == "" || names[tc.Name] || tc.Name == "all" || tc.Name == "concurrency" {
			return nil, fmt.Errorf("test case names must be nonempty, unique, and not reserved")
		}
		names[tc.Name] = true
		if filepath.Base(tc.File) != tc.File || !strings.HasSuffix(tc.File, ".hurl") {
			return nil, fmt.Errorf("invalid Hurl file for %s", tc.Name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), tc.File)); err != nil {
			return nil, err
		}
		references[tc.File] = true
		match, err := filepath.Match(selection, tc.Name)
		if err != nil {
			return nil, err
		}
		if selection == "all" || match {
			selected = append(selected, tc)
		}
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.hurl"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !references[filepath.Base(file)] {
			return nil, fmt.Errorf("%s is not registered in %s", file, path)
		}
	}
	return selected, nil
}

func runCase(ctx context.Context, binary string, config backend.Config, tc Case) (err error) {
	scenario, err := mockio.LoadScenario("testdata/demo.json")
	if err != nil {
		return err
	}
	if tc.Browser != nil {
		scenario.Browser = *tc.Browser
	}
	for id, fault := range tc.CatalogFaults {
		fixture, ok := scenario.Retailers[id]
		if !ok {
			return fmt.Errorf("unknown fixture retailer %s", id)
		}
		fixture.Fault = fault
		scenario.Retailers[id] = fixture
	}
	env, err := Start(ctx, Options{Binary: binary, Config: config, Scenario: scenario})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, env.Close()) }()
	variables := env.Variables()
	seedVariables, err := env.Seed(ctx, tc.Seeds)
	if err != nil {
		return err
	}
	for name, value := range seedVariables {
		variables[name] = value
	}
	for name, value := range tc.Variables {
		if _, reserved := variables[name]; reserved {
			return fmt.Errorf("case %s overrides harness variable %s", tc.Name, name)
		}
		variables[name] = value
	}
	largeFile := filepath.Join(env.dir, "oversized-request.json")
	if err := os.WriteFile(largeFile, []byte(`{"query":"`+strings.Repeat("x", (1<<20)+1)+`"}`), 0600); err != nil {
		return err
	}
	variables["oversized_request_file"] = largeFile
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	slices.Sort(names)
	args := []string{"--test", "--no-color", "--max-time", "3", "--file-root", env.dir}
	for _, name := range names {
		args = append(args, "--variable", name+"="+variables[name])
	}
	args = append(args, filepath.Join("tests/api", tc.File))
	cmd := exec.CommandContext(ctx, "hurl", args...)
	if data, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s\nbackend log:\n%s", data, env.logs.String())
	}
	return nil
}
