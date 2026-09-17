package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "xdf":
		return runXDFCommand(args[1:], stdout, stderr)
	case "a2l":
		return runA2LCommand(args[1:], stdout, stderr)
	case "maps":
		return runMapsCommand(args[1:], stdout, stderr)
	case "mapdef":
		return runMapdefCommand(args[1:], stdout, stderr)
	case "diff":
		return diffFiles(args[1:], stdout, stderr)
	case "identify":
		return identifyFile(args[1:], stdout)
	case "inspect":
		return inspectCommand(args[1:], stdout, stderr)
	case "checksum":
		return checksumCommand(args[1:], stdout)
	case "mhd":
		return runMhdCommand(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		return usageError()
	}
}

func runXDFCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "info":
		return xdfInfo(args[1:], stdout)
	case "convert":
		return xdfConvert(args[1:], stdout, stderr)
	default:
		return usageError()
	}
}

func runMapsCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "list":
		return mapsList(args[1:], stdout, stderr)
	case "show":
		return mapsShow(args[1:], stdout, stderr)
	case "set":
		return mapsSet(args[1:], stdout, stderr)
	default:
		return usageError()
	}
}

const usage = `FreeBeamer experimental calibration CLI

Usage:
  freebeamer xdf info XDF
  freebeamer xdf convert XDF --output DEFINITION.mapdef
  freebeamer a2l info A2L
  freebeamer a2l convert A2L --output DEFINITION.mapdef
  freebeamer maps list --xdf XDF
  freebeamer maps show --xdf XDF --bin BIN --name NAME
  freebeamer maps set --xdf XDF --bin BIN --map NAME [--row N --col N | --index N] --value VALUE --output OUTPUT
  freebeamer mapdef validate DEFINITION.mapdef
  freebeamer mapdef migrate DEFINITION.mapdef --output OUTPUT.mapdef
  freebeamer diff [--xdf XDF] OLD NEW
  freebeamer identify BIN
  freebeamer inspect --xdf XDF --bin BIN
  freebeamer checksum verify BIN
  freebeamer checksum fix BIN --output OUTPUT
  freebeamer mhd monitor [--target ADDR] [--capture-file PATH] [--source ADDR] [--timeout DURATION] [--interval DURATION] [--count N] [--upload-endpoint URL] [--api-key KEY] [--buffer-file PATH] [--device-id ID] HOST[:PORT]
`

func usageError() error {
	return errors.New(strings.TrimSpace(usage))
}
