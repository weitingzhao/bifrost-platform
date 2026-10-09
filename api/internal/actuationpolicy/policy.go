// Package actuationpolicy is the allow-list for the generic write actions.
// Concrete namespaces, repositories, and images live in config/actuation-policy.yaml.
// This package does not name them.
package actuationpolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Policy is the parsed allow-list.
type Policy struct {
	Delivery  Delivery  `yaml:"delivery"`
	Apply     Apply     `yaml:"apply"`
	Jobs      Jobs      `yaml:"jobs"`
	Probe     Probe     `yaml:"probe"`
	Cleanup   Cleanup   `yaml:"cleanup"`
	Admission Admission `yaml:"admission"`
}

// Delivery is where apply_manifest runs. The platform starts this pipeline;
// it does not apply manifests itself.
type Delivery struct {
	Namespace      string `yaml:"namespace"`
	Pipeline       string `yaml:"pipeline"`
	ServiceAccount string `yaml:"service_account"`
	GiteaBase      string `yaml:"gitea_base"`
	GiteaOrg       string `yaml:"gitea_org"`
}

// Apply is the manifest allow-list.
type Apply struct {
	Repos            []Repo            `yaml:"repos"`
	Namespaces       map[string]string `yaml:"namespaces"`
	Resources        []Resource        `yaml:"resources"`
	CicdResources    []Resource        `yaml:"cicd_resources"`
	DaemonDeployment string            `yaml:"daemon_deployment"`
}

// Repo is one git repository and the path prefixes inside it.
type Repo struct {
	Name     string   `yaml:"name"`
	Prefixes []string `yaml:"prefixes"`
}

// Resource is one namespaced kind the applier may write.
type Resource struct {
	Group    string `yaml:"group"`
	Resource string `yaml:"resource"`
	Kind     string `yaml:"kind"`
}

// Jobs is create_job_from_cronjob.
type Jobs struct {
	Deny       []string          `yaml:"deny"`
	Namespaces map[string]string `yaml:"namespaces"`
}

// Probe is run_probe_pod.
type Probe struct {
	Deny              []string          `yaml:"deny"`
	Images            []string          `yaml:"images"`
	Namespaces        map[string]string `yaml:"namespaces"`
	EnvFrom           []string          `yaml:"env_from"`
	MaxTimeoutSeconds int               `yaml:"max_timeout_seconds"`
}

// Cleanup is delete_finished_jobs.
type Cleanup struct {
	Namespaces []string `yaml:"namespaces"`
}

// Admission is the ValidatingAdmissionPolicy allow-list, kept next to the
// rest so the infra check can compare one file.
type Admission struct {
	PipelineServiceAccounts []string `yaml:"pipeline_service_accounts"`
	JobServiceAccounts      []string `yaml:"job_service_accounts"`
	ApplierPipeline         string   `yaml:"applier_pipeline"`
}

// Load reads a policy file.
func Load(path string) (*Policy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Policy
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("actuation policy: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// LoadFromConfigDir reads actuation-policy.yaml next to the other platform config.
func LoadFromConfigDir(dir string) (*Policy, error) {
	return Load(filepath.Join(dir, "actuation-policy.yaml"))
}

// Validate checks the document is usable. It does not know which names are right.
func (p *Policy) Validate() error {
	if p == nil {
		return fmt.Errorf("actuation policy is nil")
	}
	if strings.TrimSpace(p.Delivery.Pipeline) == "" || strings.TrimSpace(p.Delivery.Namespace) == "" {
		return fmt.Errorf("actuation policy: delivery pipeline and namespace are required")
	}
	if len(p.Apply.Namespaces) == 0 || len(p.Apply.Repos) == 0 {
		return fmt.Errorf("actuation policy: apply repos and namespaces are required")
	}
	if strings.TrimSpace(p.Apply.DaemonDeployment) == "" {
		return fmt.Errorf("actuation policy: daemon_deployment is required")
	}
	for _, ns := range p.Jobs.Deny {
		if _, ok := p.Jobs.Namespaces[ns]; ok {
			return fmt.Errorf("actuation policy: %q is both allowed and denied for jobs", ns)
		}
		if _, err := p.ProbeTier(ns, p.firstImage(), false); err == nil {
			return fmt.Errorf("actuation policy: job-denied namespace is allowed for probes")
		}
	}
	for _, ns := range p.Probe.Deny {
		if _, ok := p.Probe.Namespaces[ns]; ok {
			return fmt.Errorf("actuation policy: %q is both allowed and denied for probes", ns)
		}
	}
	return nil
}

func (p *Policy) firstImage() string {
	if len(p.Probe.Images) == 0 {
		return ""
	}
	return "registry.example/" + p.Probe.Images[0] + "tag"
}

// RepoAllowed reports whether repo/path may be rendered.
func (p *Policy) RepoAllowed(repo, path string) error {
	repo = strings.TrimSpace(repo)
	path = strings.Trim(strings.TrimSpace(path), "/")
	if repo == "" || path == "" {
		return fmt.Errorf("repo and path are required")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path must not contain ..")
	}
	for _, r := range p.Apply.Repos {
		if r.Name != repo {
			continue
		}
		for _, prefix := range r.Prefixes {
			prefix = strings.Trim(prefix, "/")
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return nil
			}
		}
		return fmt.Errorf("path is outside the allow-list for this repo")
	}
	return fmt.Errorf("repo is not in the allow-list")
}

// ManifestTier is the highest namespace tier in the plan. A daemon Deployment is X.
func (p *Policy) ManifestTier(namespaces []string, hasDaemon bool) (string, error) {
	if hasDaemon {
		return "X", nil
	}
	if len(namespaces) == 0 {
		return "", fmt.Errorf("plan has no namespaces")
	}
	best := ""
	for _, ns := range namespaces {
		tier, ok := p.Apply.Namespaces[ns]
		if !ok {
			return "", fmt.Errorf("namespace is not in the apply allow-list")
		}
		if tierRank(tier) > tierRank(best) {
			best = tier
		}
	}
	if best == "" {
		return "", fmt.Errorf("no tier")
	}
	return best, nil
}

// KindAllowed reports whether kind may be written into namespace.
func (p *Policy) KindAllowed(namespace, kind string) error {
	list := p.Apply.Resources
	if namespace == p.Delivery.Namespace {
		list = p.Apply.CicdResources
	}
	for _, r := range list {
		if r.Kind == kind {
			return nil
		}
	}
	return fmt.Errorf("resource kind is not allowed in this namespace")
}

// JobTier returns the tier or an error when the namespace is denied or unknown.
func (p *Policy) JobTier(namespace string) (string, error) {
	namespace = strings.TrimSpace(namespace)
	for _, d := range p.Jobs.Deny {
		if namespace == d {
			return "", fmt.Errorf("namespace is on the job deny list")
		}
	}
	tier, ok := p.Jobs.Namespaces[namespace]
	if !ok {
		return "", fmt.Errorf("namespace is not in the job allow-list")
	}
	return tier, nil
}

// ProbeTier returns the tier. envFrom is rejected outside the env_from list.
func (p *Policy) ProbeTier(namespace, image string, envFrom bool) (string, error) {
	namespace = strings.TrimSpace(namespace)
	for _, d := range p.Probe.Deny {
		if namespace == d {
			return "", fmt.Errorf("namespace is on the probe deny list")
		}
	}
	tier, ok := p.Probe.Namespaces[namespace]
	if !ok {
		return "", fmt.Errorf("namespace is not in the probe allow-list")
	}
	if !imageAllowed(p.Probe.Images, image) {
		return "", fmt.Errorf("image is not in the allow-list")
	}
	if envFrom && !contains(p.Probe.EnvFrom, namespace) {
		return "", fmt.Errorf("env_from is not allowed in this namespace")
	}
	return tier, nil
}

// CleanupAllowed reports whether finished jobs may be deleted in namespace.
func (p *Policy) CleanupAllowed(namespace string) error {
	if contains(p.Cleanup.Namespaces, strings.TrimSpace(namespace)) {
		return nil
	}
	return fmt.Errorf("namespace is not in the cleanup allow-list")
}

// Timeout clamps a probe deadline. Zero uses 60. Above the max is an error.
func (p *Policy) Timeout(seconds int) (int, error) {
	max := p.Probe.MaxTimeoutSeconds
	if max <= 0 {
		max = 900
	}
	if seconds == 0 {
		return 60, nil
	}
	if seconds < 1 || seconds > max {
		return 0, fmt.Errorf("timeout_seconds must be between 1 and %d", max)
	}
	return seconds, nil
}

func imageAllowed(prefixes []string, image string) bool {
	image = strings.TrimSpace(image)
	if image == "" || strings.Contains(image, " ") {
		return false
	}
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		idx := strings.Index(image, prefix)
		if idx < 0 {
			continue
		}
		if idx == 0 || image[idx-1] == '/' {
			return true
		}
	}
	return false
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func tierRank(t string) int {
	switch t {
	case "B":
		return 1
	case "C":
		return 2
	case "D":
		return 3
	case "X":
		return 4
	default:
		return 0
	}
}
