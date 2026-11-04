package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/freebeamer/core/pkg/a2l"
	"github.com/freebeamer/core/pkg/mapdef"
)

func runA2LCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "info":
		return a2lInfo(args[1:], stdout)
	case "convert":
		return a2lConvert(args[1:], stdout, stderr)
	default:
		return usageError()
	}
}

func a2lInfo(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: freebeamer a2l info A2L")
	}
	definition, err := a2l.Load(args[0])
	if err != nil {
		return err
	}
	module := definition.Project.Module
	fmt.Fprintf(stdout, "Project: %s\nModule: %s\n", definition.Project.Name, module.Name)
	fmt.Fprintf(stdout, "Compu methods: %d\nRecord layouts: %d\nCharacteristics: %d\nAxis pts: %d\n",
		len(module.CompuMethods), len(module.RecordLayouts), len(module.Characteristics), len(module.AxisPts))
	return nil
}

func a2lConvert(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("a2l convert", flag.ContinueOnError)
	set.SetOutput(stderr)
	output := set.String("output", "", "output .mapdef path (must not exist)")
	author := set.String("author", "", "record this author in provenance")
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
		return errors.New("usage: freebeamer a2l convert A2L --output DEFINITION.mapdef [--author NAME] [--license SPDX] [--source-url URL]")
	}
	definition, err := a2l.Load(input)
	if err != nil {
		return err
	}
	document, err := (a2l.Converter{}).Convert(definition, input)
	if err != nil {
		return err
	}
	applyProvenanceFlags(&document.Provenance, *author, *license, *sourceURL)
	if err := mapdef.Save(*output, document); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Converted %d parameters.\nOutput: %s\n", len(document.Parameters), *output)
	return nil
}
