// @target: es2022
// @module: commonjs
// @ownRuntime: ./own.mjs

// `ownRuntime` needs ES module output: with CommonJS it is an error, and it is ignored.

export shared struct S { v: number }
export function f(x: sending number[]) { return x; }
