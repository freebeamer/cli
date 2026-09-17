package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateBMWChecksumCLIEndToEnd(t *testing.T) {
	private := filepath.Join("..", "..", "testdata", "private")
	xdfPath := filepath.Join(private, "00000FEF81A001.xdf")
	stockPath := filepath.Join(private, "00000FEF81A001_original.bin")
	for _, path := range []string{xdfPath, stockPath} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			t.Skip("private BMW fixtures are not installed")
		} else if err != nil {
			t.Fatal(err)
		}
	}

	stockBefore, err := os.ReadFile(stockPath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := run([]string{"checksum", "verify", stockPath}, &stdout, &stderr); err != nil {
		t.Fatalf("verify stock: %v\n%s", err, stdout.String())
	}
	if got := strings.Count(stdout.String(), "  valid\n"); got != 11 {
		t.Fatalf("stock valid count=%d, output:\n%s", got, stdout.String())
	}

	modifiedPath := filepath.Join(t.TempDir(), "modified.bin")
	stdout.Reset()
	if err := run([]string{"maps", "set", "--xdf", xdfPath, "--bin", stockPath, "--map", "Performance gauge scaling", "--row", "0", "--col", "0", "--value", "1.2", "--output", modifiedPath}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	err = run([]string{"checksum", "verify", modifiedPath}, &stdout, &stderr)
	if err == nil || !strings.Contains(stdout.String(), "block-0x60-1-ADD16") || !strings.Contains(stdout.String(), "invalid") {
		t.Fatalf("modified verification error=%v, output:\n%s", err, stdout.String())
	}

	correctedPath := filepath.Join(t.TempDir(), "corrected.bin")
	stdout.Reset()
	if err := run([]string{"checksum", "fix", modifiedPath, "--output", correctedPath}, &stdout, &stderr); err != nil {
		t.Fatalf("fix: %v\n%s", err, stdout.String())
	}
	stdout.Reset()
	if err := run([]string{"checksum", "verify", correctedPath}, &stdout, &stderr); err != nil {
		t.Fatalf("verify corrected: %v\n%s", err, stdout.String())
	}

	corrected, err := os.ReadFile(correctedPath)
	if err != nil {
		t.Fatal(err)
	}
	const oracleSHA = "698dc5eb0a18923464d978a41880883789355c0d42f25096945e61b080f6a737"
	if got := fmt.Sprintf("%x", sha256.Sum256(corrected)); got != oracleSHA {
		t.Fatalf("corrected SHA-256=%s, want independent oracle %s", got, oracleSHA)
	}
	after, err := os.ReadFile(stockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stockBefore, after) {
		t.Fatal("stock fixture was modified")
	}
}

func TestPrivateBMWInspect(t *testing.T) {
	private := filepath.Join("..", "..", "testdata", "private")
	xdfPath := filepath.Join(private, "00000FEF81A001.xdf")
	stockPath := filepath.Join(private, "00000FEF81A001_original.bin")
	for _, path := range []string{xdfPath, stockPath} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			t.Skip("private BMW fixtures are not installed")
		} else if err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"inspect", "--xdf", xdfPath, "--bin", stockPath}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"size: 4194304", "Bosch MEVD17.2.6", "BMW N55", "version: 1.70", "categories: 73", "tables: 962", "flags: 7", "XDF region fits BIN: yes", "addressed maps in bounds: 962/962", "provider: bosch.mevd17.2.6.75P9EJ0B", "status: valid (11 regions)"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}
