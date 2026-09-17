package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/freebeamer/core/pkg/mapdef"
)

func runMapdefCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "validate":
		return mapdefValidate(args[1:], stdout, stderr)
	case "migrate":
		return mapdefMigrate(args[1:], stdout, stderr)
	default:
		return usageError()
	}
}

// mapdefValidate opens a .mapdef file and reports every Draft 2020-12 schema
// failure, located by JSON Pointer, before the file is accepted anywhere else
// in FreeBeamer.
func mapdefValidate(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if len(args) != 1 {
		return errors.New("usage: freebeamer mapdef validate DEFINITION.mapdef")
	}
	document, err := mapdef.Load(args[0])
	if err != nil {
		var schemaErrs mapdef.SchemaErrors
		if errors.As(err, &schemaErrs) {
			fmt.Fprintf(stdout, "Invalid: %d schema violation(s)\n", len(schemaErrs))
			for _, violation := range schemaErrs {
				pointer := violation.Pointer
				if pointer == "" {
					pointer = "/"
				}
				fmt.Fprintf(stdout, "  %s: %s\n", pointer, violation.Message)
			}
		}
		return err
	}
	fmt.Fprintf(stdout, "Valid: %d parameters\n", len(document.Parameters))
	return nil
}

// mapdefMigrate loads a .mapdef file, upgrades it to the current mapdef
// version via pkg/mapdef.Migrate, and writes the result to a new file. It
// never overwrites the input or an existing output path.
func mapdefMigrate(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if len(args) != 3 {
		return errors.New("usage: freebeamer mapdef migrate DEFINITION.mapdef --output OUTPUT.mapdef")
	}
	input, output := args[0], ""
	if args[1] == "--output" {
		output = args[2]
	} else if args[0] == "--output" {
		output, input = args[1], args[2]
	}
	if input == "" || output == "" {
		return errors.New("usage: freebeamer mapdef migrate DEFINITION.mapdef --output OUTPUT.mapdef")
	}
	document, err := mapdef.Load(input)
	if err != nil {
		return err
	}
	fromVersion := document.FormatVersion
	migrated, err := mapdef.Migrate(document)
	if err != nil {
		return err
	}
	if err := mapdef.Save(output, migrated); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Migrated %s -> %s\nOutput: %s\n", fromVersion, migrated.FormatVersion, output)
	return nil
}
