//// [tests/cases/compiler/ownRuntimeEmit.ts] ////

//// [own.d.mts]
export declare function share<T>(x: sending T): shared T;

//// [main.mts]
import { share } from "./own.mjs";

export shared struct Point { x: number; y: number }
shared struct Local { n: number }

export function exported(p: sending number[]): sending number[] { return p; }
function notExported() { return share([1]); }
function overloaded(x: string): string;
function overloaded(x: number): number;
function overloaded(x: any) { return x; }
declare function ambient(): void;
export default function main() { notExported(); return new Local(); }
const arrow = () => 1;
namespace NS { export function inner() {} }
function outer() { function nested() {} return nested; }

//// [structOnly.mts]
export shared struct Only { v: number }

//// [functionsOnly.mts]
export function f(x: sending number[]) { return x; }
function g() {}

//// [plain.mts]
export function plain() { return 1; }

//// [collision.mts]
export function __register(x: sending number[]) { return x; }
export shared struct S { v: number }
function __sharedStruct() {}

//// [nameless.mts]
export default function (x: sending number[]) { return x; }
function helper() {}

//// [cjs.cts]
export function c(x: sending number[]) { return x; }

//// [script.ts]
declare function send<T>(x: sending T): void;
function scriptFunction() { send([1]); }


//// [main.mjs]
import { __newStruct, register as __register, __sharedStruct } from "./own.mjs";
import { share } from "./own.mjs";
export const Point = __sharedStruct("Point", ["x", "y"], []);
const Local = __sharedStruct("Local", ["n"], []);
export function exported(p) { return p; }
function notExported() { return share([1]); }
function overloaded(x) { return x; }
export default function main() { notExported(); return __newStruct(Local); }
const arrow = () => 1;
var NS;
(function (NS) {
    function inner() { }
    NS.inner = inner;
})(NS || (NS = {}));
function outer() { function nested() { } return nested; }
__register(import.meta.url, { exported, notExported, overloaded, main, outer });
//// [structOnly.mjs]
import { __sharedStruct } from "./own.mjs";
export const Only = __sharedStruct("Only", ["v"], []);
//// [functionsOnly.mjs]
import { register as __register } from "./own.mjs";
export function f(x) { return x; }
function g() { }
__register(import.meta.url, { f, g });
//// [plain.mjs]
export function plain() { return 1; }
//// [collision.mjs]
import { register as __register_1, __sharedStruct as __sharedStruct_1 } from "./own.mjs";
export function __register(x) { return x; }
export const S = __sharedStruct_1("S", ["v"], []);
function __sharedStruct() { }
__register_1(import.meta.url, { __register, __sharedStruct });
//// [nameless.mjs]
import { register as __register } from "./own.mjs";
export default function (x) { return x; }
function helper() { }
__register(import.meta.url, { helper });
//// [cjs.cjs]
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.c = c;
function c(x) { return x; }
//// [script.js]
"use strict";
function scriptFunction() { send([1]); }
