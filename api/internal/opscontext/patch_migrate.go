package opscontext

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MigrateStreamPatch holds spine fields updated atomically on wave deliver/sign-off.
type MigrateStreamPatch struct {
	StreamID        string
	Done            int
	ReadyForSignoff int
	Status          string
	NextTask        string
	Note            string
	Headline        string
}

// PatchMigrateStream surgically updates a migrate stream and focus.headline in ops-context.yaml.
func PatchMigrateStream(path string, patch MigrateStreamPatch) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read spine for migrate patch: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse spine yaml node: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("spine yaml: unexpected document structure")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("spine yaml: root is not a mapping")
	}

	streamNode := findMigrateStreamNode(root, patch.StreamID)
	if streamNode == nil {
		return fmt.Errorf("spine yaml: migrate stream %q not found", patch.StreamID)
	}

	setMapInt(streamNode, "done", patch.Done)
	setMapInt(streamNode, "ready_for_signoff", patch.ReadyForSignoff)
	setMapScalar(streamNode, "status", patch.Status)
	setMapScalar(streamNode, "next_task", patch.NextTask)
	if patch.Note != "" {
		setMapScalar(streamNode, "note", patch.Note)
	}

	focusNode := findMapValue(root, "focus")
	if focusNode == nil {
		return fmt.Errorf("spine yaml: focus key not found")
	}
	setMapScalar(focusNode, "headline", patch.Headline)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal spine yaml: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write spine tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

func findMigrateStreamNode(root *yaml.Node, streamID string) *yaml.Node {
	tracks := findMapValue(root, "tracks")
	if tracks == nil {
		return nil
	}
	migrate := findMapValue(tracks, "migrate")
	if migrate == nil {
		return nil
	}
	streams := findMapValue(migrate, "streams")
	if streams == nil || streams.Kind != yaml.SequenceNode {
		return nil
	}
	for _, item := range streams.Content {
		idNode := findMapValue(item, "id")
		if idNode != nil && idNode.Value == streamID {
			return item
		}
	}
	return nil
}

func setMapInt(mapping *yaml.Node, key string, value int) {
	if mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Kind = yaml.ScalarNode
			mapping.Content[i+1].Tag = "!!int"
			mapping.Content[i+1].Value = fmt.Sprintf("%d", value)
			return
		}
	}
	// key missing — append
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", value)}
	mapping.Content = append(mapping.Content, keyNode, valNode)
}
