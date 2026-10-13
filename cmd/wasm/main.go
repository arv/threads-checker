//go:build js && wasm

// The compiler of the threads playground (github.com/arv/threads) in a browser. Built with
// GOOS=js GOARCH=wasm, it sets globalThis.threadsCompile(source, runtimeTypes), which compiles one
// program against the runtime's types (threads.d.ts, imported as @arv/threads) as the playground's
// dev server does with tsgo: the JavaScript with shared structs laid out (--ownRuntime), the
// ownership diagnostics `tsgo own check` reports, the type errors, and the JavaScript with its types
// only erased. It returns JSON (see result). Then it calls globalThis.threadsCompilerReady, if set.
package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/diagnosticwriter"
	"github.com/microsoft/typescript-go/internal/execute/tsc"
	"github.com/microsoft/typescript-go/internal/locale"
	"github.com/microsoft/typescript-go/internal/scanner"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/vfs"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
)

const (
	dir     = "/playground"
	program = dir + "/program.ts"
	output  = dir + "/out/program.js"
	config  = dir + "/tsconfig.json"
	runtime = "@arv/threads"
)

// the playground dev server's tsconfig.json, with ownRuntime or without it (types only erased)
func tsconfig(ownRuntime bool) string {
	own := ""
	if ownRuntime {
		own = `"ownRuntime": "` + runtime + `",`
	}
	return `{
  "compilerOptions": {
    ` + own + `
    "target": "es2022",
    "module": "esnext",
    "moduleResolution": "bundler",
    "strict": true,
    "lib": ["es2022", "dom"],
    "skipLibCheck": true,
    "outDir": "out",
    "rootDir": ".",
    "paths": {"` + runtime + `": ["./threads.d.ts"]}
  },
  "files": ["program.ts"]
}`
}

type diagnostic struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// what threadsCompile returns, as JSON: js and erased are null when nothing was emitted
type result struct {
	JS        *string      `json:"js"`
	Erased    *string      `json:"erased"`
	Ownership []diagnostic `json:"ownership"`
	Types     []diagnostic `json:"types"`
	MS        int64        `json:"ms"`
	CheckMS   int64        `json:"checkMs"`
}

// the lib files, parsed once for every compile: they are most of the work
var (
	libsMu sync.Mutex
	libs   = map[ast.SourceFileParseOptions]*ast.SourceFile{}
)

type host struct{ compiler.CompilerHost }

func (h host) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
	if !bundled.IsBundled(opts.FileName) {
		return h.CompilerHost.GetSourceFile(opts)
	}
	libsMu.Lock()
	defer libsMu.Unlock()
	if file, ok := libs[opts]; ok {
		return file
	}
	file := h.CompilerHost.GetSourceFile(opts)
	if file != nil {
		libs[opts] = file
	}
	return file
}

type configHost struct{ fs vfs.FS }

func (h configHost) FS() vfs.FS                  { return h.fs }
func (h configHost) GetCurrentDirectory() string { return dir }

// a program of source against the runtime's types, with ownRuntime or without
func newProgram(source string, runtimeTypes string, ownRuntime bool) (*compiler.Program, []*ast.Diagnostic) {
	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		program:               source,
		dir + "/threads.d.ts": runtimeTypes,
		config:                tsconfig(ownRuntime),
	}, true /*useCaseSensitiveFileNames*/))
	cache := &tsc.ExtendedConfigCache{}
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile(config, nil, nil, configHost{fs}, cache)
	if len(errors) != 0 {
		return nil, errors
	}
	return compiler.NewProgram(compiler.ProgramOptions{
		Config:         parsed,
		Host:           host{compiler.NewCachedFSCompilerHost(dir, fs, bundled.LibPath(), cache, nil)},
		SingleThreaded: core.TSTrue,
	}), nil
}

// the program's JavaScript, if it emitted any
func emit(ctx context.Context, p *compiler.Program) *string {
	var js *string
	p.Emit(ctx, compiler.EmitOptions{WriteFile: func(fileName string, text string, _ *compiler.WriteFileData) error {
		if fileName == output {
			js = &text
		}
		return nil
	}})
	return js
}

// a diagnostic as tsc --pretty false prints it, its message chain on one line
func toDiagnostic(d *ast.Diagnostic) diagnostic {
	line, col := scanner.GetECMALineAndUTF16CharacterOfPosition(d.File(), d.Pos())
	message := diagnosticwriter.FlattenDiagnosticMessage(diagnosticwriter.WrapASTDiagnostic(d), "\n", locale.Default)
	parts := strings.Split(message, "\n")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return diagnostic{Line: line + 1, Col: int(col) + 1, Code: "TS" + itoa(int(d.Code())), Message: strings.Join(parts, " ")}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func compile(source string, runtimeTypes string) result {
	ctx := context.Background()
	start := time.Now()
	r := result{Ownership: []diagnostic{}, Types: []diagnostic{}}
	p, errors := newProgram(source, runtimeTypes, true)
	if p == nil {
		for _, d := range errors {
			r.Types = append(r.Types, diagnostic{Line: 1, Col: 1, Code: "TS" + itoa(int(d.Code())), Message: d.String()})
		}
		return r
	}
	file := p.GetSourceFile(program)
	all := slices.Concat(p.GetProgramDiagnostics(), p.GetSyntacticDiagnostics(ctx, file), p.GetSemanticDiagnostics(ctx, file))
	r.CheckMS = time.Since(start).Milliseconds()
	for _, d := range compiler.SortAndDeduplicateDiagnostics(all) {
		if d.File() == nil || d.File().FileName() != program || d.Category() != diagnostics.CategoryError {
			continue
		}
		if code := d.Code(); code >= 96000 && code <= 96099 {
			r.Ownership = append(r.Ownership, toDiagnostic(d))
		} else {
			r.Types = append(r.Types, toDiagnostic(d))
		}
	}
	r.JS = emit(ctx, p)
	r.MS = time.Since(start).Milliseconds()
	if erased, _ := newProgram(source, runtimeTypes, false); erased != nil {
		r.Erased = emit(ctx, erased)
	}
	return r
}

func main() {
	js.Global().Set("threadsCompile", js.FuncOf(func(this js.Value, args []js.Value) any {
		out, err := json.Marshal(compile(args[0].String(), args[1].String()))
		if err != nil {
			panic(err)
		}
		return string(out)
	}))
	if ready := js.Global().Get("threadsCompilerReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
