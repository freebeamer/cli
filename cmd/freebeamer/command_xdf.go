package main

import (
	"errors"
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
	_ = stderr
	if len(args) != 3 {
		return errors.New("usage: freebeamer xdf convert XDF --output DEFINITION.mapdef")
	}
	input, output := args[0], ""
	if args[1] == "--output" {
		output = args[2]
	} else if args[0] == "--output" {
		output, input = args[1], args[2]
	}
	if input == "" || output == "" {
		return errors.New("usage: freebeamer xdf convert XDF --output DEFINITION.mapdef")
	}
	definition, err := xdf.Load(input)
	if err != nil {
		return err
	}
	document, err := (xdf.Converter{}).Convert(definition, input)
	if err != nil {
		return err
	}
	if err := mapdef.Save(output, document); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Converted %d parameters with %d diagnostics.\nOutput: %s\n", len(document.Parameters), len(document.ImportDiagnostics), output)
	return nil
}
