package main

import (
	"errors"
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
	_ = stderr
	if len(args) != 3 {
		return errors.New("usage: freebeamer a2l convert A2L --output DEFINITION.mapdef")
	}
	input, output := args[0], ""
	if args[1] == "--output" {
		output = args[2]
	} else if args[0] == "--output" {
		output, input = args[1], args[2]
	}
	if input == "" || output == "" {
		return errors.New("usage: freebeamer a2l convert A2L --output DEFINITION.mapdef")
	}
	definition, err := a2l.Load(input)
	if err != nil {
		return err
	}
	document, err := (a2l.Converter{}).Convert(definition, input)
	if err != nil {
		return err
	}
	if err := mapdef.Save(output, document); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Converted %d parameters.\nOutput: %s\n", len(document.Parameters), output)
	return nil
}
