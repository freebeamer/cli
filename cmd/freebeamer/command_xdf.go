package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/freebeamer/core/pkg/mapdef"
	"github.com/freebeamer/core/pkg/xdf"
)

func xdfInfo(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: freebeamer xdf info XDF")
	}
	definition, err := xdf.Load(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "XDF version: %s\n", definition.Version)
	if len(definition.Header.Regions) == 0 {
		fmt.Fprintln(stdout, "Binary region: not declared")
	} else {
		region := definition.Header.Regions[0]
		fmt.Fprintf(stdout, "Binary region: 0x%06X-0x%06X\n", region.Start, region.End())
	}
	fmt.Fprintf(stdout, "Categories: %d\nTables: %d\nFlags: %d\n", len(definition.Categories), len(definition.Tables), len(definition.Flags))
	return nil
}

func xdfConvert(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("xdf convert", flag.ContinueOnError)
	set.SetOutput(stderr)
	output := set.String("output", "", "output .mapdef path (must not exist)")
	author := set.String("author", "", "record this author in provenance (overrides the XDF header)")
	license := set.String("license", "", "record this licence in provenance (e.g. an SPDX id, or unknown)")
	sourceURL := set.String("source-url", "", "record where the source came from in provenance")
	input, rest := leadingPositional(args)
	if err := set.Parse(rest); err != nil {
		return err
	}
	if input == "" {
		input = set.Arg(0)
	}
	if input == "" || *output == "" {
		return errors.New("usage: freebeamer xdf convert XDF --output DEFINITION.mapdef [--author NAME] [--license SPDX] [--source-url URL]")
	}
	definition, err := xdf.Load(input)
	if err != nil {
		return err
	}
	document, err := (xdf.Converter{}).Convert(definition, input)
	if err != nil {
		return err
	}
	applyProvenanceFlags(&document.Provenance, *author, *license, *sourceURL)
	if err := mapdef.Save(*output, document); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Converted %d parameters with %d diagnostics.\nOutput: %s\n", len(document.Parameters), len(document.ImportDiagnostics), *output)
	return nil
}
