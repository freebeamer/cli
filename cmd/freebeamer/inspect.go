package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/freebeamer/core/pkg/binfile"
	"github.com/freebeamer/core/pkg/calibration"
	"github.com/freebeamer/core/pkg/checksum"
	"github.com/freebeamer/core/pkg/identify"
	"github.com/freebeamer/core/pkg/xdf"
)

func inspectCommand(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("inspect", flag.ContinueOnError)
	set.SetOutput(stderr)
	xdfPath := set.String("xdf", "", "path to XDF definition")
	binPath := set.String("bin", "", "path to binary")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *xdfPath == "" || *binPath == "" || set.NArg() != 0 {
		return errors.New("inspect: --xdf and --bin are required")
	}
	definition, err := xdf.Load(*xdfPath)
	if err != nil {
		return fmt.Errorf("load XDF: %w", err)
	}
	image, err := binfile.Load(*binPath)
	if err != nil {
		return fmt.Errorf("load BIN: %w", err)
	}
	document, err := (xdf.Converter{}).Convert(definition, *xdfPath)
	if err != nil {
		return fmt.Errorf("convert XDF: %w", err)
	}
	decoder, err := calibration.New(document, image)
	if err != nil {
		return err
	}
	validationErrors := decoder.Validate()
	identity := identify.Identify(image.Bytes())

	fmt.Fprintf(stdout, "ROM:\n  size: %d\n  sha256: %x\n\n", image.Size(), image.SHA256())
	fmt.Fprintln(stdout, "ECU:")
	if identity.ECUVendor == "" && identity.ECUFamily == "" {
		fmt.Fprintln(stdout, "  unknown")
	} else {
		fmt.Fprintf(stdout, "  %s\n", strings.TrimSpace(identity.ECUVendor+" "+identity.ECUFamily))
	}
	if identity.Engine != "" {
		fmt.Fprintf(stdout, "  %s\n", identity.Engine)
	}
	fmt.Fprintf(stdout, "  confidence: %s\n\n", identity.Confidence)

	fmt.Fprintf(stdout, "XDF:\n  version: %s\n", definition.Version)
	if len(definition.Header.Regions) == 0 {
		fmt.Fprintln(stdout, "  region: not declared")
	} else {
		region := definition.Header.Regions[0]
		fmt.Fprintf(stdout, "  region: 0x%06X-0x%06X\n", region.Start, region.End())
	}
	fmt.Fprintf(stdout, "  categories: %d\n  tables: %d\n  flags: %d\n\n", len(definition.Categories), len(definition.Tables), len(definition.Flags))

	fmt.Fprintln(stdout, "Compatibility:")
	fmt.Fprintf(stdout, "  XDF region fits BIN: %s\n", yesNo(definition.RegionsFit(image.Size())))
	fmt.Fprintf(stdout, "  addressed maps in bounds: %d/%d\n", len(definition.Tables)-len(validationErrors), len(definition.Tables))
	fmt.Fprintf(stdout, "  parse errors: 0\n  map validation errors: %d\n\n", len(validationErrors))

	fmt.Fprintln(stdout, "Checksums:")
	registry, err := checksumRegistry()
	if err != nil {
		return err
	}
	provider, results, err := registry.Verify(image.Bytes())
	if errors.Is(err, checksum.ErrProviderNotFound) {
		fmt.Fprintln(stdout, "  provider: none\n  status: unsupported")
		return nil
	}
	if err != nil {
		fmt.Fprintf(stdout, "  provider: %s\n  status: error (%v)\n", provider, err)
		return nil
	}
	fmt.Fprintf(stdout, "  provider: %s\n", provider)
	status := "invalid"
	if checksum.AllValid(results) {
		status = "valid"
	}
	fmt.Fprintf(stdout, "  status: %s (%d regions)\n", status, len(results))
	return nil
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
