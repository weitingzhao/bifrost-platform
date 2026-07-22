package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTopologySuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.yaml")
	if err := os.WriteFile(path, []byte(fixtureTopologyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	file, gotPath, err := LoadTopology(dir)
	if err != nil {
		t.Fatalf("LoadTopology: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if file.DeploymentPhase != "k3s_partial" {
		t.Fatalf("DeploymentPhase = %q", file.DeploymentPhase)
	}
	if len(file.Nodes) != 1 {
		t.Fatalf("Nodes = %+v", file.Nodes)
	}
	node := file.Nodes[0]
	if node.ID != "node-a" || node.Host != "10.0.0.1" || !node.InK3sCluster {
		t.Fatalf("node = %+v", node)
	}
	if node.Grid != (GridPos{Row: 1, Col: 2}) {
		t.Fatalf("Grid = %+v, want {1 2}", node.Grid)
	}
}

func TestLoadTopologyDefaultsDeploymentPhase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.yaml")
	if err := os.WriteFile(path, []byte("nodes:\n  - id: a\n    label: A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, _, err := LoadTopology(dir)
	if err != nil {
		t.Fatalf("LoadTopology: %v", err)
	}
	if file.DeploymentPhase != "compose" {
		t.Fatalf("DeploymentPhase = %q, want default compose", file.DeploymentPhase)
	}
}

func TestLoadTopologyMissingFile(t *testing.T) {
	if _, _, err := LoadTopology(t.TempDir()); err == nil {
		t.Fatal("expected error for missing topology.yaml")
	}
}

func TestLoadTopologyEmptyNodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.yaml")
	if err := os.WriteFile(path, []byte("nodes: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadTopology(dir); err == nil {
		t.Fatal("expected error for empty nodes list")
	}
}

func TestLoadTopologyEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-topology.yaml")
	if err := os.WriteFile(path, []byte(fixtureTopologyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_TOPOLOGY", path)
	_, gotPath, err := LoadTopology("/some/other/dir")
	if err != nil {
		t.Fatalf("LoadTopology: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want env override %q", gotPath, path)
	}
}

func TestTopologyDirFromConfigPath(t *testing.T) {
	if got := TopologyDirFromConfigPath("/a/b/environments.yaml"); got != "/a/b" {
		t.Fatalf("TopologyDirFromConfigPath() = %q, want /a/b", got)
	}
}
