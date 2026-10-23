package types

import (
	"encoding/json"
	"testing"
)

func TestHasEditedContent(t *testing.T) {
	manual := &Knowledge{Type: KnowledgeTypeManual}
	if !manual.HasEditedContent() {
		t.Fatal("manual knowledge always has editable content")
	}

	fileNoEdit := &Knowledge{Type: "file"}
	if fileNoEdit.HasEditedContent() {
		t.Fatal("file knowledge without metadata content must not be treated as edited")
	}

	fileEdited := &Knowledge{Type: "file"}
	if err := fileEdited.SetContentMetadata("# edited", 1, true); err != nil {
		t.Fatalf("SetContentMetadata failed: %v", err)
	}
	if !fileEdited.HasEditedContent() {
		t.Fatal("file knowledge with metadata content must be treated as edited")
	}

	var nilKnowledge *Knowledge
	if nilKnowledge.HasEditedContent() {
		t.Fatal("nil knowledge must not panic nor report edited content")
	}
}

func TestSetContentMetadataMergesAndPreservesKeys(t *testing.T) {
	k := &Knowledge{Type: "file"}
	if err := k.SetProcessOverrides(&KnowledgeProcessOverrides{}); err != nil {
		t.Fatalf("SetProcessOverrides failed: %v", err)
	}
	// An unrelated pipeline-owned key that must survive a content save.
	raw := map[string]any{}
	if err := json.Unmarshal(k.Metadata, &raw); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	raw["datasource_id"] = "ds-1"
	bytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	k.Metadata = JSON(bytes)

	if err := k.SetContentMetadata("# new body", 3, true); err != nil {
		t.Fatalf("SetContentMetadata failed: %v", err)
	}

	meta, err := k.ManualMetadata()
	if err != nil || meta == nil {
		t.Fatalf("ManualMetadata failed: %v", err)
	}
	if meta.Content != "# new body" {
		t.Fatalf("content mismatch: %q", meta.Content)
	}
	if meta.Version != 3 {
		t.Fatalf("version mismatch: %d", meta.Version)
	}
	if !meta.IndexStale {
		t.Fatal("index_stale must be true after edit")
	}
	if meta.Format != ManualKnowledgeFormatMarkdown {
		t.Fatalf("format mismatch: %q", meta.Format)
	}

	merged := map[string]any{}
	if err := json.Unmarshal(k.Metadata, &merged); err != nil {
		t.Fatalf("unmarshal merged metadata: %v", err)
	}
	if merged["datasource_id"] != "ds-1" {
		t.Fatal("SetContentMetadata dropped an unrelated metadata key")
	}
	if _, ok := merged["process_overrides"]; !ok {
		t.Fatal("SetContentMetadata dropped process_overrides")
	}
}

func TestClearIndexStale(t *testing.T) {
	k := &Knowledge{Type: "file"}
	if err := k.SetContentMetadata("body", 2, true); err != nil {
		t.Fatalf("SetContentMetadata failed: %v", err)
	}
	if err := k.ClearIndexStale(); err != nil {
		t.Fatalf("ClearIndexStale failed: %v", err)
	}
	meta, err := k.ManualMetadata()
	if err != nil || meta == nil {
		t.Fatalf("ManualMetadata failed: %v", err)
	}
	if meta.IndexStale {
		t.Fatal("index_stale must be cleared")
	}
	if meta.Content != "body" || meta.Version != 2 {
		t.Fatal("ClearIndexStale must keep content and version")
	}

	// No flag at all: no-op, no error.
	empty := &Knowledge{Type: "file"}
	if err := empty.ClearIndexStale(); err != nil {
		t.Fatalf("ClearIndexStale on empty metadata must be a no-op: %v", err)
	}
}

func TestManualMetadataParsesIndexStale(t *testing.T) {
	k := &Knowledge{Type: KnowledgeTypeManual}
	meta := NewManualKnowledgeMetadata("hello", ManualKnowledgeStatusPublish, 1)
	meta.IndexStale = true
	if err := k.SetManualMetadata(meta); err != nil {
		t.Fatalf("SetManualMetadata failed: %v", err)
	}
	parsed, err := k.ManualMetadata()
	if err != nil || parsed == nil {
		t.Fatalf("ManualMetadata failed: %v", err)
	}
	if !parsed.IndexStale {
		t.Fatal("index_stale must round-trip through metadata JSON")
	}
}
