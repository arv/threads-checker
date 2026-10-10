//// [tests/cases/compiler/ownRuntimeCommonJS.ts] ////

//// [ownRuntimeCommonJS.ts]
// `ownRuntime` needs ES module output: with CommonJS it is an error, and it is ignored.

export shared struct S { v: number }
export function f(x: sending number[]) { return x; }


//// [ownRuntimeCommonJS.js]
"use strict";
// `ownRuntime` needs ES module output: with CommonJS it is an error, and it is ignored.
Object.defineProperty(exports, "__esModule", { value: true });
exports.S = void 0;
exports.f = f;
exports.S = __sharedStruct("S", ["v"]);
function f(x) { return x; }
