//// [tests/cases/compiler/ownRuntimeEmit.ts] ////

//// [threads.d.mts]
declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;
export interface Cell {
  readonly [cellBrand]: true;
}
export type Sending<T> = T;
export type Shared<T> = T extends Cell | ((...args: any[]) => any) ? T
  : T extends object ? { readonly [K in keyof T]: Shared<T[K]> }
  : T;
export declare function share<T>(value: Sending<T>): Shared<T>;
export interface Field<T> {
  readonly [fieldBrand]: T;
}
export declare const f64: Field<number>;
export interface StructType<T> {
  new (): T & Cell;
  readonly fields: readonly string[];
}
export type Struct<S> = S extends StructType<infer T> ? T & Cell : never;
export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;

//// [main.mts]
import { f64, type Sending, share, struct } from "./threads.mjs";

export const Point = struct({ x: f64, y: f64 });
const Local = struct({ n: f64 });

// Registered: function declarations with a body, exported or not, once for overloads. Not
// registered: ambient functions, arrow functions, and functions in namespaces or other functions.
export function exported(p: Sending<number[]>): Sending<number[]> { return p; }
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
// A struct and no functions: the struct's helper is imported, and nothing is registered.
import { f64, struct } from "./threads.mjs";
export const Only = struct({ v: f64 });

//// [functionsOnly.mts]
// Only a type-only import, which is elided: the functions are still registered.
import type { Sending } from "./threads.mjs";
export function f(x: Sending<number[]>) { return x; }
function g() {}

//// [plain.mts]
// Does not import the runtime: nothing is imported or registered.
export function plain() { return 1; }

//// [collision.mts]
// Declares the helpers' names itself: the helpers get unique names.
import { f64, type Sending, struct } from "./threads.mjs";
export function __register(x: Sending<number[]>) { return x; }
export const S = struct({ v: f64 });
function __sharedStruct() {}

//// [nameless.mts]
// A default-exported function without a name is not registered.
import type { Sending } from "./threads.mjs";
export default function (x: Sending<number[]>) { return x; }
function helper() {}

//// [cjs.cts]
// Emitted as CommonJS: nothing is imported or registered.
import type { Sending } from "./threads.mjs";
export function c(x: Sending<number[]>) { return x; }

//// [script.ts]
// A script cannot import the runtime: nothing is registered.
function scriptFunction() { return [1]; }


//// [main.mjs]
import { newStruct as __newStruct, register as __register, sharedStruct as __sharedStruct } from "./threads.mjs";
import { f64, share, struct } from "./threads.mjs";
export const Point = __sharedStruct("Point", ["x", "y"], []);
const Local = __sharedStruct("Local", ["n"], []);
// Registered: function declarations with a body, exported or not, once for overloads. Not
// registered: ambient functions, arrow functions, and functions in namespaces or other functions.
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
import { sharedStruct as __sharedStruct } from "./threads.mjs";
// A struct and no functions: the struct's helper is imported, and nothing is registered.
import { f64, struct } from "./threads.mjs";
export const Only = __sharedStruct("Only", ["v"], []);
//// [functionsOnly.mjs]
import { register as __register } from "./threads.mjs";
export function f(x) { return x; }
function g() { }
__register(import.meta.url, { f, g });
//// [plain.mjs]
// Does not import the runtime: nothing is imported or registered.
export function plain() { return 1; }
//// [collision.mjs]
import { register as __register_1, sharedStruct as __sharedStruct_1 } from "./threads.mjs";
// Declares the helpers' names itself: the helpers get unique names.
import { f64, struct } from "./threads.mjs";
export function __register(x) { return x; }
export const S = __sharedStruct_1("S", ["v"], []);
function __sharedStruct() { }
__register_1(import.meta.url, { __register, __sharedStruct });
//// [nameless.mjs]
import { register as __register } from "./threads.mjs";
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
// A script cannot import the runtime: nothing is registered.
function scriptFunction() { return [1]; }
