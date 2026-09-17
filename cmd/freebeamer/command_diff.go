package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/freebeamer/core/pkg/binfile"
	"github.com/freebeamer/core/pkg/xdf"
)

func diffFiles(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("diff", flag.ContinueOnError)
	set.SetOutput(stderr)
	xdfPath := set.String("xdf", "", "optional XDF definition")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 2 {
		return errors.New("usage: freebeamer diff [--xdf XDF] OLD NEW")
	}
	oldImage, err := binfile.Load(set.Arg(0))
	if err != nil {
		return err
	}
	newImage, err := binfile.Load(set.Arg(1))
	if err != nil {
		return err
	}
	changes, err := binfile.DiffBytes(oldImage.Bytes(), newImage.Bytes())
	if err != nil {
		return err
	}
	if *xdfPath != "" {
		if _, err := xdf.Load(*xdfPath); err != nil {
			return err
		}
	}
	fmt.Fprintln(stdout, "OFFSET  ORIGINAL  CURRENT")
	for _, change := range changes {
		fmt.Fprintf(stdout, "0x%08X  0x%02X      0x%02X\n", change.Offset, change.Original, change.Current)
	}
	fmt.Fprintf(stdout, "Changed bytes: %d\n", len(changes))
	return nil
}
