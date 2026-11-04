package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/freebeamer/core/pkg/mapdef"
	"github.com/freebeamer/core/pkg/types"
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
	case "review":
		return mapdefReview(args[1:], stdout, stderr)
	case "strip-source":
		return mapdefStripSource(args[1:], stdout, stderr)
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

// mapdefReview loads a .mapdef file and reports what sharing it would disclose
// — author, licence, source digest, the user's own redistribution assertion,
// any embedded verbatim source, and free-text fields that may hold personal or
// vehicle data — plus whether it is safe to share. This is engineering
// diligence, not legal advice. With --require-shareable the command exits
// non-zero unless the review is "ok".
func mapdefReview(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("mapdef review", flag.ContinueOnError)
	set.SetOutput(stderr)
	requireShareable := set.Bool("require-shareable", false, "exit non-zero unless the review is ok")
	asJSON := set.Bool("json", false, "print the review as JSON")
	input, rest := leadingPositional(args)
	if err := set.Parse(rest); err != nil {
		return err
	}
	if input == "" {
		input = set.Arg(0)
	}
	if input == "" {
		return errors.New("usage: freebeamer mapdef review DEFINITION.mapdef [--require-shareable] [--json]")
	}
	document, err := mapdef.Load(input)
	if err != nil {
		return err
	}
	review := mapdef.ReviewForSharing(document)
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(review); err != nil {
			return err
		}
	} else {
		printReview(stdout, review)
	}
	if *requireShareable && review.Severity != "ok" {
		return fmt.Errorf("not shareable: %s", strings.Join(review.Reasons, "; "))
	}
	return nil
}

func printReview(stdout io.Writer, review mapdef.SharingReview) {
	fmt.Fprintf(stdout, "Sharing review: %s\n", review.Severity)
	fmt.Fprintf(stdout, "  redistribution: %s (your own unverified assertion)\n", review.Redistribution)
	fmt.Fprintf(stdout, "  licence:        %s\n", valueOrDash(review.License))
	fmt.Fprintf(stdout, "  author:         %s\n", valueOrDash(review.Author))
	fmt.Fprintf(stdout, "  source:         %s\n", valueOrDash(review.Source))
	fmt.Fprintf(stdout, "  source sha256:  %s\n", valueOrDash(review.SourceSHA256))
	if review.EmbeddedSource != nil {
		fmt.Fprintf(stdout, "  embedded source: %s, %d bytes — sharing this file shares that source verbatim\n",
			review.EmbeddedSource.Format, review.EmbeddedSource.Bytes)
	} else {
		fmt.Fprintf(stdout, "  embedded source: none\n")
	}
	if len(review.FreeTextFields) > 0 {
		fmt.Fprintf(stdout, "  check for personal/vehicle data in: %s\n", strings.Join(review.FreeTextFields, ", "))
	}
	for _, reason := range review.Reasons {
		fmt.Fprintf(stdout, "  - %s\n", reason)
	}
	fmt.Fprintln(stdout, "This is engineering diligence, not legal advice; conversion grants no licence.")
}

// leadingPositional pulls a single leading non-flag argument (the input
// path, conventionally first) out of args so the remaining flags parse with
// flag.FlagSet, which otherwise stops at the first positional. Flags may also
// follow the input, or precede it (then the input is left for set.Arg(0)).
func leadingPositional(args []string) (positional string, rest []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// mapdefStripSource writes a new .mapdef with every embedded verbatim source
// (extensions.*.rawDocument) removed, keeping provenance.sourceSha256 and
// recording the removal. It never overwrites an existing path (mapdef.Save is
// exclusive), so the original, from which the source stays recoverable, is
// preserved. Stripping does not make the result redistributable.
func mapdefStripSource(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("mapdef strip-source", flag.ContinueOnError)
	set.SetOutput(stderr)
	output := set.String("output", "", "new .mapdef path to write (must not exist)")
	input, rest := leadingPositional(args)
	if err := set.Parse(rest); err != nil {
		return err
	}
	if input == "" {
		input = set.Arg(0)
	}
	if input == "" || *output == "" {
		return errors.New("usage: freebeamer mapdef strip-source DEFINITION.mapdef --output OUTPUT.mapdef")
	}
	document, err := mapdef.Load(input)
	if err != nil {
		return err
	}
	before := mapdef.ReviewForSharing(document).EmbeddedSource
	stripped, err := mapdef.StripSource(document)
	if err != nil {
		return err
	}
	if err := mapdef.Save(*output, stripped); err != nil {
		return err
	}
	if before == nil {
		fmt.Fprintf(stdout, "No embedded source to remove.\nOutput: %s\n", *output)
	} else {
		fmt.Fprintf(stdout, "Removed embedded %s source (%d bytes); sourceSha256 retained.\nOutput: %s\n", before.Format, before.Bytes, *output)
	}
	fmt.Fprintln(stdout, "Stripping does not make this definition shareable; its parameters can still derive from protected source material.")
	return nil
}

// applyProvenanceFlags overrides a converted document's provenance from
// convert-time flags. Empty flags leave the converter's own values in place;
// nothing is ever inferred from the file or where it came from.
func applyProvenanceFlags(provenance *types.MapProvenance, author, license, sourceURL string) {
	if strings.TrimSpace(author) != "" {
		provenance.Author = author
	}
	if strings.TrimSpace(license) != "" {
		provenance.License = license
	}
	if strings.TrimSpace(sourceURL) != "" {
		provenance.Source = sourceURL
	}
}
