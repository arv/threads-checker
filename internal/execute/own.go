package execute

import (
	"context"
	"fmt"
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/collections"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/execute/tsc"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
)

// `tsgo own check [tsc options]`: the ownership checker of the own/shared threads extension, run like
// a linter. It builds the program as tsc does (`-p`, or the tsconfig.json it finds) and reports only
// the extension's diagnostics (codes 96000-96099: ownership, typed layouts, reference counting), in
// tsc's format; ordinary type errors are tsc's job. `ownRuntime`, which stock tsc does not accept in
// a tsconfig.json, is given on the command line (`--ownRuntime ./threads.mjs`), or in a tsconfig that
// only tsgo reads (one that extends the project's).
//
// Exit status: 0 without ownership diagnostics, 1 with some, 3 if the project cannot be checked.

const (
	ownDiagnosticsFirst = 96000
	ownDiagnosticsLast  = 96099
)

func ownCommandLine(ctx context.Context, sys tsc.System, args []string) tsc.CommandLineResult {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(sys.Writer(), "Usage: tsgo own check [-p <tsconfig.json>] [--ownRuntime <module>] [tsc options]")
		fmt.Fprintln(sys.Writer(), "  Reports the ownership diagnostics of the own/shared threads extension (TS96000-TS96099).")
		return tsc.CommandLineResult{Status: tsc.ExitStatusInvalidProject_OutputsSkipped}
	}
	commandLine := tsoptions.ParseCommandLine(args[1:], sys)
	reportDiagnostic := tsc.CreateDiagnosticReporter(sys, sys.Writer(), commandLine.Locale(), commandLine.CompilerOptions())
	invalid := func(diagnostics []*ast.Diagnostic) tsc.CommandLineResult {
		for _, d := range diagnostics {
			reportDiagnostic(d)
		}
		return tsc.CommandLineResult{Status: tsc.ExitStatusInvalidProject_OutputsSkipped}
	}
	if len(commandLine.Errors) > 0 {
		return invalid(commandLine.Errors)
	}

	config := commandLine
	if configFileName, diagnostic := findOwnConfigFile(sys, commandLine); diagnostic != nil {
		return invalid([]*ast.Diagnostic{diagnostic})
	} else if configFileName != "" {
		var raw *collections.OrderedMap[string, any]
		if options, ok := commandLine.Raw.(*collections.OrderedMap[string, any]); ok {
			raw = &collections.OrderedMap[string, any]{}
			raw.Set("compilerOptions", options)
		}
		parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile(configFileName, commandLine.CompilerOptions(), raw, sys, &tsc.ExtendedConfigCache{})
		if len(errors) != 0 {
			return invalid(errors)
		}
		config = parsed
	}
	if config.CompilerOptions().OwnRuntime == "" {
		fmt.Fprintln(sys.Writer(), "tsgo own check: no ownRuntime. Give the module programs import the runtime from: --ownRuntime ./threads.mjs")
		return tsc.CommandLineResult{Status: tsc.ExitStatusInvalidProject_OutputsSkipped}
	}
	reportDiagnostic = tsc.CreateDiagnosticReporter(sys, sys.Writer(), config.Locale(), config.CompilerOptions())

	host := compiler.NewCachedFSCompilerHost(sys.GetCurrentDirectory(), sys.FS(), sys.DefaultLibraryPath(), &tsc.ExtendedConfigCache{}, nil /*trace*/)
	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
	// Every file is checked, whatever other errors the program has (tsc stops at syntax errors).
	all := slices.Concat(program.GetConfigFileParsingDiagnostics(), program.GetProgramDiagnostics(), program.GetSemanticDiagnostics(ctx, nil))
	var diagnostics []*ast.Diagnostic
	for _, d := range all {
		if code := d.Code(); code >= ownDiagnosticsFirst && code <= ownDiagnosticsLast {
			diagnostics = append(diagnostics, d)
		}
	}
	diagnostics = compiler.SortAndDeduplicateDiagnostics(diagnostics)
	for _, d := range diagnostics {
		reportDiagnostic(d)
	}
	tsc.CreateReportErrorSummary(sys, config.Locale(), config.CompilerOptions())(diagnostics)
	if len(diagnostics) > 0 {
		return tsc.CommandLineResult{Status: tsc.ExitStatusDiagnosticsPresent_OutputsSkipped}
	}
	return tsc.CommandLineResult{Status: tsc.ExitStatusSuccess}
}

// The tsconfig.json to check: the one `-p` names (a file or a directory), or the nearest one from the
// current directory unless files are given.
func findOwnConfigFile(sys tsc.System, commandLine *tsoptions.ParsedCommandLine) (string, *ast.Diagnostic) {
	if project := commandLine.CompilerOptions().Project; project != "" {
		fileOrDirectory := tspath.NormalizePath(project)
		if sys.FS().DirectoryExists(fileOrDirectory) {
			fileOrDirectory = tspath.CombinePaths(fileOrDirectory, "tsconfig.json")
		}
		if !sys.FS().FileExists(fileOrDirectory) {
			return "", ast.NewCompilerDiagnostic(diagnostics.The_specified_path_does_not_exist_Colon_0, fileOrDirectory)
		}
		return fileOrDirectory, nil
	}
	if len(commandLine.FileNames()) != 0 {
		return "", nil
	}
	configFileName := findConfigFile(tspath.NormalizePath(sys.GetCurrentDirectory()), sys.FS().FileExists, "tsconfig.json")
	if configFileName == "" {
		return "", ast.NewCompilerDiagnostic(diagnostics.Cannot_find_a_tsconfig_json_file_at_the_current_directory_Colon_0, tspath.NormalizePath(sys.GetCurrentDirectory()))
	}
	return configFileName, nil
}
