package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/isty2e/daem/internal/buildidentity"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/internal/releaseartifact"
)

type commandOptions struct {
	binaryPath string
	outputDir  string
	version    string
	revision   string
	goVersion  string
	goos       string
	goarch     string
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	options, err := parseOptions(args, stderr)
	if err != nil {
		return 2
	}
	target, err := platformsupport.ParseTarget(options.goos, options.goarch)
	if err != nil {
		fmt.Fprintf(stderr, "releasepack: invalid target: %v\n", err)
		return 2
	}
	requirement, err := buildidentity.NewReleaseRequirement(options.version, options.revision, options.goVersion)
	if err != nil {
		fmt.Fprintf(stderr, "releasepack: invalid release requirement: %v\n", err)
		return 2
	}
	executable, err := readStableRegularFile(options.binaryPath)
	if err != nil {
		fmt.Fprintf(stderr, "releasepack: read executable: %v\n", err)
		return 1
	}
	artifact, err := releaseartifact.Build(executable, requirement, target)
	if err != nil {
		fmt.Fprintf(stderr, "releasepack: build artifact: %v\n", err)
		return 1
	}
	if err := publishArtifactDirectory(options.outputDir, artifact); err != nil {
		fmt.Fprintf(stderr, "releasepack: publish artifact directory: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintf(
		stdout,
		"archive=%s\nchecksum=%s\nsha256=%s\n",
		filepath.Join(options.outputDir, artifact.ArchiveName()),
		filepath.Join(options.outputDir, artifact.ChecksumName()),
		artifact.SHA256(),
	); err != nil {
		fmt.Fprintf(stderr, "releasepack: report outputs: %v\n", err)
		return 1
	}
	return 0
}

func parseOptions(args []string, stderr io.Writer) (commandOptions, error) {
	var options commandOptions
	flags := flag.NewFlagSet("releasepack", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.binaryPath, "binary", "", "path to the daem executable")
	flags.StringVar(&options.outputDir, "output-dir", "", "new directory that will receive the archive and checksum")
	flags.StringVar(&options.version, "version", "", "exact canonical release tag")
	flags.StringVar(&options.revision, "revision", "", "exact full Git revision")
	flags.StringVar(&options.goVersion, "go-version", "", "exact Go toolchain version")
	flags.StringVar(&options.goos, "goos", "", "expected executable GOOS")
	flags.StringVar(&options.goarch, "goarch", "", "expected executable GOARCH")
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, err
	}
	if flags.NArg() != 0 {
		err := fmt.Errorf("unexpected positional argument %q", flags.Arg(0))
		fmt.Fprintf(stderr, "releasepack: %v\n", err)
		return commandOptions{}, err
	}
	required := []struct {
		name  string
		value string
	}{
		{name: "--binary", value: options.binaryPath},
		{name: "--output-dir", value: options.outputDir},
		{name: "--version", value: options.version},
		{name: "--revision", value: options.revision},
		{name: "--go-version", value: options.goVersion},
		{name: "--goos", value: options.goos},
		{name: "--goarch", value: options.goarch},
	}
	for _, field := range required {
		if field.value == "" {
			fmt.Fprintf(stderr, "releasepack: %s is required\n", field.name)
			return commandOptions{}, fmt.Errorf("%s is required", field.name)
		}
	}
	return options, nil
}
