//go:build e2e && pipeline

// Package pipeline drives the Osano provider through the Pulumi engine: it builds and installs the
// provider plugin, runs a Pulumi YAML program with the pulumi CLI against a stateful mock of the
// Osano Customer REST API, and asserts on engine steps, stack outputs, and the requests the mock saw.
//
// The suite runs the pulumi CLI directly and reads its --event-log rather than using the Automation
// API, whose Go package needs modules this repository does not otherwise depend on.
package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
)

const (
	providerModule     = "github.com/jflavan/pulumi-osano"
	providerMainPkg    = providerModule + "/provider/cmd/pulumi-resource-osano"
	providerVersionVar = providerModule + "/provider/version.Version"

	typeConfig      = "osano:index:CookieConsentConfig"
	typeRule        = "osano:index:CookieConsentRule"
	typePublication = "osano:index:CookieConsentPublication"
	typeProvider    = "pulumi:providers:osano"

	stackName = "e2e"
)

// harness holds what every scenario shares: the pulumi CLI and an isolated Pulumi home with the
// provider plugins installed.
type harness struct {
	pulumiBin  string
	pulumiHome string
	env        map[string]string
}

// newHarness builds the provider once per version, installs each build as a plugin into a fresh
// PULUMI_HOME, and points the Pulumi CLI at a local file backend so no Pulumi Cloud account is used.
func newHarness(ctx context.Context, t *testing.T, versions ...string) *harness {
	t.Helper()
	pulumiBin, err := exec.LookPath("pulumi")
	if err != nil {
		t.Fatalf("the pulumi CLI must be on PATH: %v", err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go toolchain must be on PATH: %v", err)
	}
	repoRoot := findRepoRoot(t)

	root := t.TempDir()
	h := &harness{pulumiBin: pulumiBin, pulumiHome: filepath.Join(root, "pulumi-home")}
	stateDir := filepath.Join(root, "state")
	for _, dir := range []string{h.pulumiHome, stateDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	//nolint:gosec // A passphrase for throwaway local test state, not a credential.
	h.env = map[string]string{
		"PULUMI_HOME":                   h.pulumiHome,
		"PULUMI_BACKEND_URL":            "file://" + filepath.ToSlash(stateDir),
		"PULUMI_CONFIG_PASSPHRASE":      "pipeline-e2e-passphrase",
		"PULUMI_SKIP_UPDATE_CHECK":      "true",
		"PULUMI_IGNORE_AMBIENT_PLUGINS": "true",
		// Enables the --event-log flag, as the Automation API does.
		"PULUMI_DEBUG_COMMANDS": "true",
		// Fail instead of downloading anything: every plugin the program needs is installed below.
		"PULUMI_DISABLE_AUTOMATIC_PLUGIN_ACQUISITION": "true",
		// The provider prefers these over stack config; clear them so a developer's real credentials
		// or overrides never reach the mock.
		"OSANO_API_KEY":             "",
		"OSANO_UC_API_KEY":          "",
		"OSANO_API_BASE_URL":        "",
		"OSANO_API_TIMEOUT_SECONDS": "",
	}
	for key, value := range h.env {
		t.Setenv(key, value)
	}

	for _, version := range versions {
		binary := buildProvider(ctx, t, goBin, repoRoot, filepath.Join(root, "build", version), version)
		h.installPlugin(ctx, t, binary, version)
	}
	return h
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		//nolint:gosec // Reads go.mod files in the directories above the test package.
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && declaresModule(data, providerModule) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find the %s module root above the test directory", providerModule)
		}
		dir = parent
	}
}

// declaresModule reports whether a go.mod declares module. It compares whole lines with any line
// ending, so a checkout with CRLF line endings (git autocrlf on Windows) is found too.
func declaresModule(goMod []byte, module string) bool {
	for _, line := range bytes.Split(goMod, []byte("\n")) {
		if string(bytes.TrimSpace(line)) == "module "+module {
			return true
		}
	}
	return false
}

func buildProvider(ctx context.Context, t *testing.T, goBin, repoRoot, outDir, version string) string {
	t.Helper()
	binary := filepath.Join(outDir, "pulumi-resource-osano")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	start := time.Now()
	//nolint:gosec // Builds this repository's provider with a test-chosen version stamp.
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", binary,
		"-ldflags", "-X "+providerVersionVar+"="+version, providerMainPkg)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build of the provider (version %s) failed: %v\n%s", version, err, out)
	}
	t.Logf("built provider %s in %s", version, time.Since(start).Round(time.Millisecond))
	return binary
}

func (h *harness) installPlugin(ctx context.Context, t *testing.T, binary, version string) {
	t.Helper()
	if _, err := h.run(ctx, "", "plugin", "install", "resource", "osano", version,
		"--file", binary, "--exact", "--reinstall"); err != nil {
		t.Fatalf("install plugin osano %s: %v", version, err)
	}
	pluginDir := filepath.Join(h.pulumiHome, "plugins", "resource-osano-v"+version)
	if _, err := os.Stat(pluginDir); err != nil {
		t.Fatalf("plugin osano %s was not installed into %s: %v", version, pluginDir, err)
	}
}

// processEnv is the current environment with the harness overrides applied.
func (h *harness) processEnv() []string {
	env := make([]string, 0, len(os.Environ())+len(h.env))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := h.env[key]; !overridden {
			env = append(env, entry)
		}
	}
	for key, value := range h.env {
		env = append(env, key+"="+value)
	}
	return env
}

// run executes the pulumi CLI in dir and returns its stdout. The error includes stdout and stderr.
func (h *harness) run(ctx context.Context, dir string, args ...string) (string, error) {
	args = append(args, "--non-interactive")
	//nolint:gosec // Runs the pulumi CLI found on PATH with test-controlled arguments.
	cmd := exec.CommandContext(ctx, h.pulumiBin, args...)
	cmd.Dir = dir
	cmd.Env = h.processEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("pulumi %s: %w\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// project is one Pulumi YAML project directory with a single stack.
type project struct {
	h        *harness
	dir      string
	logDir   string
	template string
	mock     *mockOsano
	ops      int
}

// newProject writes the program for version into a fresh directory and creates its stack.
func (h *harness) newProject(
	ctx context.Context, t *testing.T, mock *mockOsano, templateFile, version string,
) *project {
	t.Helper()
	template, err := os.ReadFile(filepath.Join("testdata", templateFile)) //nolint:gosec // Test fixture path.
	if err != nil {
		t.Fatal(err)
	}
	p := &project{h: h, dir: t.TempDir(), logDir: t.TempDir(), template: string(template), mock: mock}
	p.writeProgram(t, version)
	if _, err := h.run(ctx, p.dir, "stack", "init", stackName); err != nil {
		t.Fatalf("create stack: %v", err)
	}
	return p
}

// writeProgram renders the Pulumi.yaml template with the provider version the program pins.
func (p *project) writeProgram(t *testing.T, version string) {
	t.Helper()
	program := strings.ReplaceAll(p.template, "{{PROVIDER_VERSION}}", version)
	if err := os.WriteFile(filepath.Join(p.dir, "Pulumi.yaml"), []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
}

// configValue is one stack config entry.
type configValue struct {
	Value  string
	Secret bool
}

func (p *project) setConfig(ctx context.Context, t *testing.T, values map[string]configValue) {
	t.Helper()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, 4+2*len(keys))
	args = append(args, "config", "set-all", "--stack", stackName)
	for _, key := range keys {
		flag := "--plaintext"
		if values[key].Secret {
			flag = "--secret"
		}
		args = append(args, flag, key+"="+values[key].Value)
	}
	if _, err := p.h.run(ctx, p.dir, args...); err != nil {
		t.Fatalf("set stack config: %v", err)
	}
}

// stepInfo aggregates the engine events for one resource during one operation.
type stepInfo struct {
	URN          string
	Type         string
	Ops          []apitype.OpType
	Keys         []string
	Diffs        []string
	DetailedDiff map[string]apitype.PropertyDiff
}

func (s *stepInfo) String() string {
	detailed := make([]string, 0, len(s.DetailedDiff))
	for path, diff := range s.DetailedDiff {
		detailed = append(detailed, fmt.Sprintf("%s:%s", path, diff.Kind))
	}
	sort.Strings(detailed)
	return fmt.Sprintf("%s ops=%v diffs=%v detailedDiff=%v replaceKeys=%v", s.URN, s.Ops, s.Diffs, detailed, s.Keys)
}

// opResult is what one engine operation did: its per-resource steps, output, and the mock requests it caused.
type opResult struct {
	Label       string
	Steps       map[string]*stepInfo
	Diagnostics []string
	StdOut      string
	Outputs     map[string]any
	Requests    []recordedRequest
	Duration    time.Duration
}

func readEventLog(t *testing.T, path string) []apitype.EngineEvent {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // The event log this test asked the CLI to write.
	if err != nil {
		t.Logf("no engine event log at %s: %v", path, err)
		return nil
	}
	var evs []apitype.EngineEvent
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event apitype.EngineEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode engine event %q: %v", line, err)
		}
		evs = append(evs, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read engine event log: %v", err)
	}
	return evs
}

func summarize(evs []apitype.EngineEvent) (steps map[string]*stepInfo, diagnostics []string) {
	steps = map[string]*stepInfo{}
	add := func(md *apitype.StepEventMetadata) {
		step, ok := steps[md.URN]
		if !ok {
			step = &stepInfo{URN: md.URN, Type: md.Type, DetailedDiff: map[string]apitype.PropertyDiff{}}
			steps[md.URN] = step
		}
		if !slices.Contains(step.Ops, md.Op) {
			step.Ops = append(step.Ops, md.Op)
		}
		for _, key := range md.Keys {
			if !slices.Contains(step.Keys, key) {
				step.Keys = append(step.Keys, key)
			}
		}
		for _, key := range md.Diffs {
			if !slices.Contains(step.Diffs, key) {
				step.Diffs = append(step.Diffs, key)
			}
		}
		for path, diff := range md.DetailedDiff {
			step.DetailedDiff[path] = diff
		}
	}
	for i := range evs {
		event := &evs[i]
		switch {
		case event.ResourcePreEvent != nil:
			add(&event.ResourcePreEvent.Metadata)
		case event.ResOutputsEvent != nil:
			add(&event.ResOutputsEvent.Metadata)
		case event.ResOpFailedEvent != nil:
			add(&event.ResOpFailedEvent.Metadata)
		case event.DiagnosticEvent != nil:
			if severity := event.DiagnosticEvent.Severity; severity == "error" || severity == "warning" {
				diagnostics = append(diagnostics, fmt.Sprintf("[%s] %s %s", severity,
					event.DiagnosticEvent.URN, strings.TrimSpace(event.DiagnosticEvent.Message)))
			}
		}
	}
	return steps, diagnostics
}

// operation runs one engine operation (up, preview, refresh, destroy) and records what it did.
func (p *project) operation(ctx context.Context, t *testing.T, label string, args ...string) *opResult {
	t.Helper()
	p.ops++
	eventLog := filepath.Join(p.logDir, fmt.Sprintf("op-%02d-events.jsonl", p.ops))
	args = append(args, "--stack", stackName, "--color", "never", "--event-log", eventLog)

	mark, start := p.mock.Mark(), time.Now()
	stdout, err := p.h.run(ctx, p.dir, args...)
	steps, diagnostics := summarize(readEventLog(t, eventLog))
	res := &opResult{
		Label:       label,
		Steps:       steps,
		Diagnostics: diagnostics,
		StdOut:      stdout,
		Requests:    p.mock.RequestsSince(mark),
		Duration:    time.Since(start),
	}
	t.Logf("%s (pulumi %s) took %s; steps:\n%s\nOsano requests:\n%s", label, args[0],
		res.Duration.Round(time.Millisecond), res.describeSteps(), describeRequests(res.Requests))
	if len(diagnostics) > 0 {
		t.Logf("%s diagnostics:\n  %s", label, strings.Join(diagnostics, "\n  "))
	}
	if err != nil {
		t.Fatalf("%s failed: %v", label, err)
	}
	return res
}

func (p *project) up(ctx context.Context, t *testing.T, label string, extra ...string) *opResult {
	t.Helper()
	res := p.operation(ctx, t, label, append([]string{"up", "--yes", "--skip-preview", "--diff"}, extra...)...)
	res.Outputs = p.outputs(ctx, t)
	return res
}

func (p *project) preview(ctx context.Context, t *testing.T, label string, extra ...string) *opResult {
	t.Helper()
	return p.operation(ctx, t, label, append([]string{"preview", "--diff"}, extra...)...)
}

func (p *project) refresh(ctx context.Context, t *testing.T, label string) *opResult {
	t.Helper()
	return p.operation(ctx, t, label, "refresh", "--yes", "--skip-preview", "--diff")
}

func (p *project) destroy(ctx context.Context, t *testing.T, label string) *opResult {
	t.Helper()
	return p.operation(ctx, t, label, "destroy", "--yes", "--skip-preview")
}

func (p *project) outputs(ctx context.Context, t *testing.T) map[string]any {
	t.Helper()
	stdout, err := p.h.run(ctx, p.dir, "stack", "output", "--json", "--show-secrets", "--stack", stackName)
	if err != nil {
		t.Fatalf("read stack outputs: %v", err)
	}
	outputs := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &outputs); err != nil {
		t.Fatalf("decode stack outputs %q: %v", stdout, err)
	}
	return outputs
}

// resources returns the resources recorded in the stack's current checkpoint.
func (p *project) resources(ctx context.Context, t *testing.T) []apitype.ResourceV3 {
	t.Helper()
	stdout, err := p.h.run(ctx, p.dir, "stack", "export", "--stack", stackName)
	if err != nil {
		t.Fatalf("export stack: %v", err)
	}
	var exported apitype.UntypedDeployment
	if err := json.Unmarshal([]byte(stdout), &exported); err != nil {
		t.Fatalf("decode exported stack: %v", err)
	}
	var deployment apitype.DeploymentV3
	if err := json.Unmarshal(exported.Deployment, &deployment); err != nil {
		t.Fatalf("decode exported deployment: %v", err)
	}
	return deployment.Resources
}

func resourceOfType(t *testing.T, resources []apitype.ResourceV3, typ string) apitype.ResourceV3 {
	t.Helper()
	var found []apitype.ResourceV3
	for i := range resources {
		if string(resources[i].Type) == typ {
			found = append(found, resources[i])
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one %s in the stack state, found %d", typ, len(found))
	}
	return found[0]
}

func (r *opResult) describeSteps() string {
	if len(r.Steps) == 0 {
		return "  (no resource steps)"
	}
	urns := make([]string, 0, len(r.Steps))
	for urn := range r.Steps {
		urns = append(urns, urn)
	}
	sort.Strings(urns)
	lines := make([]string, 0, len(urns))
	for _, urn := range urns {
		lines = append(lines, "  "+r.Steps[urn].String())
	}
	return strings.Join(lines, "\n")
}

// stepsOfType returns the steps for every resource of typ, ordered by URN.
func (r *opResult) stepsOfType(typ string) []*stepInfo {
	var steps []*stepInfo
	for _, step := range r.Steps {
		if step.Type == typ {
			steps = append(steps, step)
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].URN < steps[j].URN })
	return steps
}

// requireOps fails unless the single resource of typ went through exactly the wanted operations.
func (r *opResult) requireOps(t *testing.T, typ string, want ...apitype.OpType) {
	t.Helper()
	steps := r.stepsOfType(typ)
	if len(steps) != 1 {
		t.Errorf("%s: expected one %s step, got %d:\n%s", r.Label, typ, len(steps), r.describeSteps())
		return
	}
	got := slices.Clone(steps[0].Ops)
	slices.Sort(got)
	expected := slices.Clone(want)
	slices.Sort(expected)
	if !slices.Equal(got, expected) {
		t.Errorf("%s: %s ops = %v, want %v\n  %s", r.Label, typ, steps[0].Ops, want, steps[0])
	}
}

// requireAllSame fails unless every resource step in the operation was "same".
func (r *opResult) requireAllSame(t *testing.T) {
	t.Helper()
	if len(r.Steps) == 0 {
		t.Errorf("%s: no resource steps were reported", r.Label)
	}
	for _, step := range r.Steps {
		if len(step.Ops) != 1 || step.Ops[0] != apitype.OpSame {
			t.Errorf("%s: expected no changes, but %s", r.Label, step)
		}
	}
}

var replacementOps = []apitype.OpType{
	apitype.OpReplace, apitype.OpCreateReplacement, apitype.OpDeleteReplaced,
	apitype.OpDiscardReplaced, apitype.OpImportReplacement,
}

// requireNoOsanoReplacement fails if any Osano resource was replaced, deleted, or recreated.
func (r *opResult) requireNoOsanoReplacement(t *testing.T) {
	t.Helper()
	for _, step := range r.Steps {
		if !strings.HasPrefix(step.Type, "osano:") {
			continue
		}
		for _, op := range step.Ops {
			if slices.Contains(replacementOps, op) || op == apitype.OpCreate || op == apitype.OpDelete {
				t.Errorf("%s: Osano resource was not kept in place: %s", r.Label, step)
			}
		}
	}
}

// requireUserAgent fails unless every Osano request in the operation came from the given plugin version.
func (r *opResult) requireUserAgent(t *testing.T, version string) {
	t.Helper()
	want := "pulumi-osano/" + version
	for _, request := range r.Requests {
		if request.UserAgent != want {
			t.Errorf("%s: request %s was sent by %q, want %q", r.Label, request, request.UserAgent, want)
		}
	}
}

func (r *opResult) count(filter requestFilter) int {
	return countRequests(r.Requests, filter)
}

func (r *opResult) outputMap(t *testing.T, key string) map[string]any {
	t.Helper()
	value, ok := r.Outputs[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: stack output %q is %T, want an object (outputs: %v)", r.Label, key, r.Outputs[key], r.Outputs)
	}
	return value
}

// hasOp reports whether the single resource of typ went through op.
func (r *opResult) hasOp(typ string, op apitype.OpType) bool {
	for _, step := range r.stepsOfType(typ) {
		if slices.Contains(step.Ops, op) {
			return true
		}
	}
	return false
}

func (r *opResult) output(t *testing.T, key string) string {
	t.Helper()
	value, ok := r.Outputs[key]
	if !ok {
		t.Fatalf("%s: stack output %q is missing (outputs: %v)", r.Label, key, r.Outputs)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("%s: stack output %q is %T, want string", r.Label, key, value)
	}
	return text
}
