package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freebeamer/core/pkg/mapdef"
)

func TestConvertRecordsProvenanceFlags(t *testing.T) {
	output := filepath.Join(t.TempDir(), "d.mapdef")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"xdf", "convert", fixture(), "--output", output, "--author", "K M", "--license", "CC-BY-4.0", "--source-url", "https://example.test/x.xdf"}, &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	document, err := mapdef.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if document.Provenance.Author != "K M" || document.Provenance.License != "CC-BY-4.0" || document.Provenance.Source != "https://example.test/x.xdf" {
		t.Fatalf("provenance = %#v", document.Provenance)
	}
}

func TestMapdefReviewReportsAndGates(t *testing.T) {
	output := filepath.Join(t.TempDir(), "d.mapdef")
	var buf, stderr bytes.Buffer
	if err := run([]string{"xdf", "convert", fixture(), "--output", output}, &buf, &stderr); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := run([]string{"mapdef", "review", output}, &buf, &stderr); err != nil {
		t.Fatalf("review: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"review-required", "redistribution: unknown", "embedded source: xdf", "not asserted as permitted"} {
		if !strings.Contains(out, want) {
			t.Fatalf("review output missing %q:\n%s", want, out)
		}
	}

	// --require-shareable fails on a review-required document.
	buf.Reset()
	if err := run([]string{"mapdef", "review", output, "--require-shareable"}, &buf, &stderr); err == nil {
		t.Fatal("--require-shareable should error on review-required")
	}
}

func TestMapdefStripSourceRemovesEmbeddedSource(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "d.mapdef")
	out := filepath.Join(dir, "stripped.mapdef")
	var buf, stderr bytes.Buffer
	if err := run([]string{"xdf", "convert", fixture(), "--output", in}, &buf, &stderr); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"mapdef", "strip-source", in, "--output", out}, &buf, &stderr); err != nil {
		t.Fatalf("strip-source: %v\n%s", err, stderr.String())
	}
	stripped, err := mapdef.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if mapdef.ReviewForSharing(stripped).EmbeddedSource != nil {
		t.Fatal("stripped document still has an embedded source")
	}
	if stripped.Provenance.SourceSHA256 == "" {
		t.Fatal("sourceSha256 must be retained")
	}
	// The original is untouched (exclusive write would also prevent overwrite).
	original, err := mapdef.Load(in)
	if err != nil {
		t.Fatal(err)
	}
	if mapdef.ReviewForSharing(original).EmbeddedSource == nil {
		t.Fatal("original must still carry its embedded source")
	}
}
