# threads-checker

A fork of typescript-go, the native TypeScript 7 compiler, for
[threads for JavaScript](https://github.com/arv/threads): programs written in
plain TypeScript against the [@arv/threads](https://github.com/arv/threads) API
have their ownership checked and their shared structs compiled.

- **`tsgo own check -p tsconfig.json --ownRuntime @arv/threads`** reports only
  ownership errors (use after send, an alias of something sent, a write to a
  shared value, sending what the caller still holds) and leaves type errors to
  tsc. It runs in CI like a linter, and exits 1 when it finds any.
- **`tsgo -p tsconfig.json --ownRuntime @arv/threads`** compiles: a shared
  struct's fields and a SharedArray's elements become typed-array reads and
  writes on the shared heap, references to them are counted, and each module
  registers the functions its threads run. @arv/threads' Vite plugin runs it
  (its `tsgo` option names the binary).

[@arv/threads-check](https://github.com/arv/threads/tree/main/src/checker)
reports the same ownership errors on stock TypeScript 7.1, without this fork;
`vp run compare` in arv/threads checks that the two agree. The compiler has no
such stand-in: without it, programs run with their types erased, and shared
structs are much slower.

## Building

```sh
go build -o built/local/tsgo ./cmd/tsgo
```

The threads playground also runs it in a browser, compiled to WebAssembly
(`cmd/wasm`; arv/threads' `vp run build-wasm` builds it):

```sh
GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o threads.wasm ./cmd/wasm
```

## The changes

The first commit is typescript-go as microsoft/TypeScript had it in `tsc/` at
[1e4744d682](https://github.com/microsoft/TypeScript/commit/1e4744d68260a7cb91b62b12edc3f6a2187faaf1)
(TypeScript 7.0.2); every commit after it is the fork's. Most of the fork is in
new files:

| File | What it does |
| ---- | ------------ |
| `internal/checker/ownership.go` | the ownership analysis, region by region |
| `internal/checker/ownlayout.go` | typed layouts of shared structs and SharedArrays |
| `internal/checker/ownrefcount.go` | which references are counted, and where |
| `internal/checker/ownmarkers.go`, `ownintegers.go` | the plain TypeScript API (`Sending`, `Shared`, `consume`, `struct`), and the rule that a value stored into an i32 or a u8 is an integer expression |
| `internal/transformers/tstransforms/ownlayout.go` | lowers struct and SharedArray accesses to the shared heap |
| `internal/transformers/ownruntime.go` | imports the runtime's helpers, registers thread functions |
| `internal/execute/own.go` | `tsgo own check` |
| `cmd/wasm/main.go` | the playground's compiler in a browser: one program from memory, compiled, checked and with its types erased |
| `internal/diagnostics/ownership.go` | the messages (codes 96000-96099) |
| `internal/parser/ownshared.go` | an earlier syntax for the same model (`sending T`, `shared T`, `consume`, `shared struct`), which `examples/own-shared` uses |

The `ownRuntime` compiler option names the runtime module. The tests are
`testdata/tests/cases/compiler/own*.ts` and their baselines, run with
`go test ./internal/testrunner -run TestLocal/own`.

The rest of this README is typescript-go's.

---

# TypeScript 7

[Not sure what this is? Read the announcement post!](https://devblogs.microsoft.com/typescript/typescript-native-port/)

## Preview

A preview build is available on npm as [`@typescript/native-preview`](https://www.npmjs.com/package/@typescript/native-preview).

```sh
npm install @typescript/native-preview
npx tsgo # Use this as you would tsc.
```

For TypeScript 7.0 RC and later, the command name is `tsc`.

A preview VS Code extension is [available on the VS Code marketplace](https://marketplace.visualstudio.com/items?itemName=TypeScriptTeam.native-preview).

To use this, set this in your VS Code settings:

```json
{
    "js/ts.experimental.useTsgo": true
}
```

## What Works So Far?

This is still a work in progress and is not yet at full feature parity with TypeScript. Bugs may exist. Please check this list carefully before logging a new issue or assuming an intentional change.

| Feature | Status | Notes |
|---------|--------|-------|
| Program creation | done | Same files and module resolution as TS 6.0. Not all resolution modes supported yet. |
| Parsing/scanning | done | Exact same syntax errors as TS 6.0 |
| Commandline and `tsconfig.json` parsing | done | Done, though `tsconfig` errors may not be as helpful. |
| Type resolution | done | Same types as TS 6.0. |
| Type checking | done | Same errors, locations, and messages as TS 6.0. Types printback in errors may display differently. |
| JavaScript-specific inference and JSDoc | done | Complete, but intentionally lacking some features. Declaration emit differs greatly, intentionally, to be closer to TS declarations. |
| JSX | done | - |
| Declaration emit | done | - |
| Emit (JS output) | done | - |
| Watch mode | prototype | Watches files and rebuilds, but no incremental rechecking. Not optimized. |
| Build mode / project references | done | - |
| Incremental build | done | - |
| Language service (LSP) | in progress | Nearly all features implemented. |
| API | not ready | - |

Definitions:

 * **done** aka "believed done": We're not currently aware of any deficits or major work left to do. OK to log bugs
 * **in progress**: currently being worked on; some features may work and some might not. OK to log panics, but nothing else please
 * **prototype**: proof-of-concept only; do not log bugs
 * **not ready**: either haven't even started yet, or far enough from ready that you shouldn't bother messing with it yet

## Other Notes

Long-term, we expect that this repo and its contents will be merged into `microsoft/TypeScript`.
As a result, the repo and issue tracker for typescript-go will eventually be closed, so treat discussions/issues accordingly.

For a list of intentional changes with respect to TypeScript 6.0, see CHANGES.md.

## Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit [Contributor License Agreements](https://cla.opensource.microsoft.com).

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft
trademarks or logos is subject to and must follow
[Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/legal/intellectualproperty/trademarks/usage/general).
Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship.
Any use of third-party trademarks or logos are subject to those third-party's policies.
