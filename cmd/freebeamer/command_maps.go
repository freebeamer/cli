package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strings"
	"text/tabwriter"

	"github.com/freebeamer/core/pkg/binfile"
	"github.com/freebeamer/core/pkg/calibration"
	"github.com/freebeamer/core/pkg/types"
	"github.com/freebeamer/core/pkg/xdf"
)

func mapsSet(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("maps set", flag.ContinueOnError)
	set.SetOutput(stderr)
	xdfPath := set.String("xdf", "", "path to XDF definition")
	binPath := set.String("bin", "", "path to original binary")
	mapName := set.String("map", "", "exact map title")
	row := set.Int("row", -1, "zero-based row")
	column := set.Int("col", -1, "zero-based column")
	index := set.Int("index", -1, "zero-based flat index")
	value := set.Float64("value", math.NaN(), "engineering value")
	output := set.String("output", "", "new output path")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *xdfPath == "" || *binPath == "" || *mapName == "" || *output == "" || math.IsNaN(*value) {
		return errors.New("maps set: --xdf, --bin, --map, --value, and --output are required")
	}
	if set.NArg() != 0 {
		return fmt.Errorf("maps set: unexpected arguments: %s", strings.Join(set.Args(), " "))
	}
	usingIndex := *index >= 0
	usingCell := *row >= 0 || *column >= 0
	if usingIndex == usingCell {
		return errors.New("maps set: specify either --index or both --row and --col")
	}
	if usingCell && (*row < 0 || *column < 0) {
		return errors.New("maps set: --row and --col must be specified together")
	}
	definition, err := xdf.ImportMapDefinition(*xdfPath)
	if err != nil {
		return err
	}
	image, err := binfile.Load(*binPath)
	if err != nil {
		return err
	}
	parameter, err := calibration.FindParameter(definition, *mapName)
	if err != nil {
		return err
	}
	decoder, err := calibration.New(definition, image)
	if err != nil {
		return err
	}
	var result *types.CalibrationEditResult
	if usingIndex {
		result, err = decoder.SetIndex(*parameter, *index, *value)
	} else {
		result, err = decoder.SetCell(*parameter, *row, *column, *value)
	}
	if err != nil {
		return err
	}
	if err := image.SaveAs(*output); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "requested: %g\nencoded:   %g\nraw:       %d\noffset:    0x%X\noutput:    %s\n", result.Requested, result.Actual, result.Raw, result.Offset, *output)
	return nil
}

func mapsShow(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("maps show", flag.ContinueOnError)
	set.SetOutput(stderr)
	xdfPath := set.String("xdf", "", "path to XDF definition")
	binPath := set.String("bin", "", "path to binary image")
	name := set.String("name", "", "exact map title")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *xdfPath == "" || *binPath == "" || *name == "" {
		return errors.New("maps show: --xdf, --bin, and --name are required")
	}
	if set.NArg() != 0 {
		return fmt.Errorf("maps show: unexpected arguments: %s", strings.Join(set.Args(), " "))
	}
	definition, err := xdf.ImportMapDefinition(*xdfPath)
	if err != nil {
		return err
	}
	image, err := binfile.Load(*binPath)
	if err != nil {
		return err
	}
	parameter, err := calibration.FindParameter(definition, *name)
	if err != nil {
		return err
	}
	decoder, err := calibration.New(definition, image)
	if err != nil {
		return err
	}
	decoded, err := decoder.DecodeParameter(*parameter)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Title: %s\nCategory: %s\n", parameter.Name, strings.Join(definition.CategoryNames(parameter.Categories), ", "))
	printAxisInfo(stdout, "X", parameter.Axes["x"])
	printAxisInfo(stdout, "Y", parameter.Axes["y"])
	printParameterInfo(stdout, "Z", *parameter)
	rows, columns := len(decoded.Z), 0
	if rows > 0 {
		columns = len(decoded.Z[0])
	}
	fmt.Fprintf(stdout, "Dimensions: %dx%d\n", rows, columns)
	fmt.Fprintf(stdout, "X raw: %v\nX decoded: %v\nY raw: %v\nY decoded: %v\n", decoded.Raw.X, decoded.X, decoded.Raw.Y, decoded.Y)
	fmt.Fprintln(stdout, "Z raw:")
	for _, row := range decoded.Raw.Z {
		fmt.Fprintln(stdout, row)
	}
	fmt.Fprintln(stdout, "Z decoded:")
	for _, row := range decoded.Z {
		fmt.Fprintln(stdout, row)
	}
	return nil
}

func printAxisInfo(writer io.Writer, name string, axis types.MapAxis) {
	address := "not embedded"
	if axis.Layout != nil && axis.Layout.Address != nil {
		address = fmt.Sprintf("0x%X", *axis.Layout.Address)
	}
	fmt.Fprintf(writer, "%s axis: address=%s units=%q formula=%q\n", name, address, axis.Unit, axis.Conversion.Expression)
}

func printParameterInfo(writer io.Writer, name string, parameter types.MapParameter) {
	address := "not embedded"
	if parameter.Layout.Address != nil {
		address = fmt.Sprintf("0x%X", *parameter.Layout.Address)
	}
	fmt.Fprintf(writer, "%s axis: address=%s units=%q formula=%q\n", name, address, parameter.Unit, parameter.Conversion.Expression)
}

func mapsList(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("maps list", flag.ContinueOnError)
	set.SetOutput(stderr)
	path := set.String("xdf", "", "path to XDF definition")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("maps list: --xdf is required")
	}
	if set.NArg() != 0 {
		return fmt.Errorf("maps list: unexpected arguments: %s", strings.Join(set.Args(), " "))
	}
	definition, err := xdf.ImportMapDefinition(*path)
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "INDEX\tCATEGORY\tTITLE")
	for index, parameter := range definition.Parameters {
		if parameter.Kind == "flag" {
			continue
		}
		categories := definition.CategoryNames(parameter.Categories)
		fmt.Fprintf(writer, "%d\t%s\t%s\n", index, strings.Join(categories, ", "), parameter.Name)
	}
	return writer.Flush()
}
