package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freebeamer/core/pkg/checksum"
	"github.com/freebeamer/core/pkg/mapdef"
)

func fixture() string { return filepath.Join("..", "..", "testdata", "public", "xdf", "basic.xdf") }

func a2lFixture() string { return filepath.Join("..", "..", "testdata", "public", "a2l", "basic.a2l") }

func TestXDFInfo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"xdf", "info", fixture()}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"XDF version: 1.70", "Binary region: 0x000000-0x3FFFFF", "Categories: 2", "Tables: 1", "Flags: 1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestXDFConvert(t *testing.T) {
	output := filepath.Join(t.TempDir(), "definition.mapdef")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"xdf", "convert", fixture(), "--output", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	document, err := mapdef.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if document.Format != mapdef.Format || len(document.Parameters) != 2 {
		t.Fatalf("document = %#v", document)
	}
	if !strings.Contains(stdout.String(), "Converted 2 parameters") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if err := run([]string{"xdf", "convert", fixture(), "--output", output}, &stdout, &stderr); err == nil {
		t.Fatal("existing output was overwritten")
	}
}

func TestA2LInfo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"a2l", "info", a2lFixture()}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Project: SyntheticExample", "Module: Example", "Compu methods: 7", "Record layouts: 9", "Characteristics: 16", "Axis pts: 2"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestA2LConvert(t *testing.T) {
	output := filepath.Join(t.TempDir(), "definition.mapdef")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"a2l", "convert", a2lFixture(), "--output", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	document, err := mapdef.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if document.Format != mapdef.Format || len(document.Parameters) != 16 {
		t.Fatalf("document = %#v", document)
	}
	if !strings.Contains(stdout.String(), "Converted 16 parameters") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if err := run([]string{"a2l", "convert", a2lFixture(), "--output", output}, &stdout, &stderr); err == nil {
		t.Fatal("existing output was overwritten")
	}
}

func TestMapsList(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"maps", "list", "--xdf", fixture()}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INDEX", "CATEGORY", "TITLE", "Boost", "Boost target"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestMapsShow(t *testing.T) {
	data := make([]byte, 0x208)
	binary.LittleEndian.PutUint16(data[0x100:], 100)
	binary.LittleEndian.PutUint16(data[0x102:], 200)
	for index, value := range []uint16{10, 20, 30, 40} {
		binary.LittleEndian.PutUint16(data[0x200+index*2:], value)
	}
	binPath := filepath.Join(t.TempDir(), "fixture.bin")
	if err := os.WriteFile(binPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{"maps", "show", "--xdf", fixture(), "--bin", binPath, "--name", "Boost target"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Title: Boost target", "Category: Boost", "X axis: address=0x100 units=\"rpm\" formula=\"X*10\"", "Z axis: address=0x200 units=\"kPa\" formula=\"X*0.1\"", "Dimensions: 2x2", "X raw: [100 200]", "X decoded: [1000 2000]", "[10 20]", "[1 2]"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestMapsSetAndDiff(t *testing.T) {
	data := make([]byte, 0x208)
	input := filepath.Join(t.TempDir(), "original.bin")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "modified.bin")
	var stdout, stderr bytes.Buffer
	args := []string{"maps", "set", "--xdf", fixture(), "--bin", input, "--map", "Boost target", "--row", "0", "--col", "1", "--value", "12.34", "--output", output}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"requested: 12.34", "encoded:   12.3", "raw:       123", "offset:    0x202"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("set output missing %q:\n%s", want, stdout.String())
		}
	}
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, data) {
		t.Fatal("input was modified")
	}
	modified, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if modified[0x202] != 123 || modified[0x203] != 0 {
		t.Fatalf("modified bytes=%x", modified[0x202:0x204])
	}
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"diff", input, output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"0x00000202", "0x00", "0x7B", "Changed bytes: 1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("diff output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestMapsSetRefusesExistingOutput(t *testing.T) {
	data := make([]byte, 0x208)
	input := filepath.Join(t.TempDir(), "original.bin")
	output := filepath.Join(t.TempDir(), "exists.bin")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	err := run([]string{"maps", "set", "--xdf", fixture(), "--bin", input, "--map", "Boost target", "--index", "0", "--value", "1", "--output", output}, &buffer, &buffer)
	if err == nil {
		t.Fatal("existing output overwritten")
	}
	got, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "keep" {
		t.Fatalf("existing output=%q", got)
	}
}

func TestMapdefValidate(t *testing.T) {
	valid := filepath.Join("..", "..", "examples", "synthetic.mapdef")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"mapdef", "validate", valid}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Valid: 2 parameters") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	invalid := filepath.Join(t.TempDir(), "invalid.mapdef")
	document := `{"format":"freehorse.mapdef","formatVersion":"1.0.0","id":"BAD ID","name":"Test",` +
		`"compatibility":{},"provenance":{},"memory":{"baseOffset":0},"parameters":[]}`
	if err := os.WriteFile(invalid, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	err := run([]string{"mapdef", "validate", invalid}, &stdout, &stderr)
	if err == nil {
		t.Fatal("invalid mapdef file was accepted")
	}
	var schemaErrs mapdef.SchemaErrors
	if !errors.As(err, &schemaErrs) {
		t.Fatalf("error = %v (%T), want mapdef.SchemaErrors", err, err)
	}
	for _, want := range []string{"Invalid: 1 schema violation(s)", "/id:", "pattern"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestMapdefMigrate(t *testing.T) {
	input := filepath.Join(t.TempDir(), "old.mapdef")
	document := `{"format":"freehorse.mapdef","formatVersion":"1.0.0","id":"gear","name":"Gear",` +
		`"compatibility":{},"provenance":{},"memory":{"baseOffset":0},"parameters":[{` +
		`"id":"p","kind":"scalar","name":"P",` +
		`"layout":{"dataType":"uint8","byteOrder":"not-applicable","dimensions":[1],"order":"not-applicable"},` +
		`"conversion":{"language":"freehorse-expression-v1","expression":"X"}}]}`
	if err := os.WriteFile(input, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "migrated.mapdef")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"mapdef", "migrate", input, "--output", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Migrated 1.0.0 -> "+mapdef.CurrentVersion) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	migrated, err := mapdef.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.FormatVersion != mapdef.CurrentVersion {
		t.Fatalf("FormatVersion = %q, want %q", migrated.FormatVersion, mapdef.CurrentVersion)
	}
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original), `"formatVersion":"1.0.0"`) {
		t.Fatal("migrate modified its input file")
	}
	if err := run([]string{"mapdef", "migrate", input, "--output", output}, &stdout, &stderr); err == nil {
		t.Fatal("existing output was overwritten")
	}
}

func TestIdentify(t *testing.T) {
	data := make([]byte, 4*1024*1024)
	copy(data[0x1000:], "Bosch MEVD17.2.6 BMW N55 75P9EJ0B FEF81A001")
	path := filepath.Join(t.TempDir(), "misleading-name.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"identify", path}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Manufacturer: BMW", "Vendor: Bosch", "ECU: MEVD17.2.6", "Engine family: BMW N55", "Confidence: high", "75P9EJ0B", "FEF81A001"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestInspectReportsCompatibilityAndUnsupportedChecksum(t *testing.T) {
	data := make([]byte, 0x208)
	path := filepath.Join(t.TempDir(), "small.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"inspect", "--xdf", fixture(), "--bin", path}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ROM:", "size: 520", "XDF:", "version: 1.70", "XDF region fits BIN: no", "addressed maps in bounds: 1/1", "provider: none", "status: unsupported"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestChecksumCommandsRefuseWithoutProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.bin")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "must-not-exist.bin")
	var buffer bytes.Buffer
	for _, args := range [][]string{{"checksum", "verify", path}, {"checksum", "fix", path, "--output", output}} {
		err := run(args, &buffer, &buffer)
		if !errors.Is(err, checksum.ErrProviderNotFound) {
			t.Fatalf("run(%v) error=%v", args, err)
		}
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output created or stat failed: %v", err)
	}
}

func TestCLIRejectsMissingArguments(t *testing.T) {
	var output bytes.Buffer
	for _, args := range [][]string{{}, {"xdf", "info"}, {"xdf", "convert"}, {"a2l"}, {"a2l", "info"}, {"a2l", "convert"}, {"maps", "list"}, {"maps", "show"}, {"maps", "set"}, {"mapdef"}, {"mapdef", "validate"}, {"mapdef", "migrate"}, {"diff"}, {"identify"}, {"inspect"}, {"checksum"}, {"checksum", "verify"}, {"checksum", "fix"}, {"unknown"}} {
		if err := run(args, &output, &output); err == nil {
			t.Errorf("run(%v) unexpectedly succeeded", args)
		}
	}
}
