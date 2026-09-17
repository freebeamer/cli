package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/freebeamer/core/pkg/binfile"
	"github.com/freebeamer/core/pkg/identify"
)

func identifyFile(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: freebeamer identify BIN")
	}
	image, err := binfile.Load(args[0])
	if err != nil {
		return err
	}
	identity := identify.Identify(image.Bytes())
	valueOrUnknown := func(value string) string {
		if value == "" {
			return "unknown"
		}
		return value
	}
	fmt.Fprintf(
		stdout,
		"Manufacturer: %s\nVendor: %s\nECU: %s\nEngine family: %s\nConfidence: %s\n",
		valueOrUnknown(identity.Manufacturer),
		valueOrUnknown(identity.ECUVendor),
		valueOrUnknown(identity.ECUFamily),
		valueOrUnknown(identity.Engine),
		identity.Confidence,
	)
	if len(identity.Software) > 0 {
		fmt.Fprintf(stdout, "Software: %s\n", strings.Join(identity.Software, ", "))
	}
	return nil
}
