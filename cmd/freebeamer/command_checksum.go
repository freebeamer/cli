package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/freebeamer/core/pkg/binfile"
	"github.com/freebeamer/core/pkg/checksum"
	"github.com/freebeamer/core/pkg/checksum/bmw/mevd1726"
	"github.com/freebeamer/core/pkg/types"
)

func checksumRegistry() (*checksum.Registry, error) {
	return checksum.NewRegistry(mevd1726.New())
}

func checksumCommand(args []string, stdout io.Writer) error {
	if len(args) < 1 {
		return errors.New("usage: freebeamer checksum verify BIN | checksum fix BIN --output OUTPUT")
	}
	registry, err := checksumRegistry()
	if err != nil {
		return err
	}
	switch args[0] {
	case "verify":
		return verifyChecksum(args[1:], stdout, registry)
	case "fix":
		return fixChecksum(args[1:], stdout, registry)
	default:
		return errors.New("usage: freebeamer checksum verify BIN | checksum fix BIN --output OUTPUT")
	}
}

func verifyChecksum(args []string, stdout io.Writer, registry *checksum.Registry) error {
	if len(args) != 1 {
		return errors.New("usage: freebeamer checksum verify BIN")
	}
	image, err := binfile.Load(args[0])
	if err != nil {
		return err
	}
	provider, results, err := registry.Verify(image.Bytes())
	if err != nil {
		return err
	}
	printChecksumResults(stdout, provider, results)
	if !checksum.AllValid(results) {
		return errors.New("checksum verification failed")
	}
	return nil
}

func fixChecksum(args []string, stdout io.Writer, registry *checksum.Registry) error {
	input, output, err := parseChecksumFixArgs(args)
	if err != nil {
		return err
	}
	image, err := binfile.Load(input)
	if err != nil {
		return err
	}
	provider, corrected, results, err := registry.CorrectAndVerify(image.Bytes())
	if err != nil {
		return err
	}
	if uint64(len(corrected)) != image.Size() {
		return errors.New("checksum provider changed binary size")
	}
	if err := image.Write(0, corrected); err != nil {
		return err
	}
	if err := image.SaveAs(output); err != nil {
		return err
	}
	printChecksumResults(stdout, provider, results)
	fmt.Fprintf(stdout, "Output: %s\n", output)
	return nil
}

func parseChecksumFixArgs(args []string) (string, string, error) {
	var input, output string
	for index := 0; index < len(args); index++ {
		if args[index] == "--output" {
			if index+1 >= len(args) || output != "" {
				return "", "", errors.New("checksum fix: --output requires one path")
			}
			output = args[index+1]
			index++
			continue
		}
		if strings.HasPrefix(args[index], "-") {
			return "", "", fmt.Errorf("checksum fix: unknown option %s", args[index])
		}
		if input != "" {
			return "", "", errors.New("checksum fix: exactly one input BIN is required")
		}
		input = args[index]
	}
	if input == "" || output == "" {
		return "", "", errors.New("usage: freebeamer checksum fix BIN --output OUTPUT")
	}
	return input, output, nil
}

func printChecksumResults(writer io.Writer, provider string, results []types.ChecksumResult) {
	fmt.Fprintf(writer, "Provider: %s\n", provider)
	fmt.Fprintln(writer, "NAME  REGION  STORAGE  STORED  CALCULATED  STATUS")
	for _, result := range results {
		status := "invalid"
		if result.Valid {
			status = "valid"
		}
		fmt.Fprintf(writer, "%s  0x%X-0x%X  0x%X  0x%X  0x%X  %s\n", result.Name, result.RegionStart, result.RegionEnd, result.StorageOffset, result.Stored, result.Calculated, status)
	}
}
