// @target: es2022
// @module: esnext, nodenext
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// With `ownRuntime`, ES modules that import the runtime import the runtime helpers they need and
// register their top-level functions. Modules that do not import it, CommonJS files and scripts are
// left alone.

// @filename: threads.d.mts
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

// @filename: main.mts
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

// @filename: structOnly.mts
// A struct and no functions: the struct's helper is imported, and nothing is registered.
import { f64, struct } from "./threads.mjs";
export const Only = struct({ v: f64 });

// @filename: functionsOnly.mts
// Only a type-only import, which is elided: the functions are still registered.
import type { Sending } from "./threads.mjs";
export function f(x: Sending<number[]>) { return x; }
function g() {}

// @filename: plain.mts
// Does not import the runtime: nothing is imported or registered.
export function plain() { return 1; }

// @filename: collision.mts
// Declares the helpers' names itself: the helpers get unique names.
import { f64, type Sending, struct } from "./threads.mjs";
export function __register(x: Sending<number[]>) { return x; }
export const S = struct({ v: f64 });
function __sharedStruct() {}

// @filename: nameless.mts
// A default-exported function without a name is not registered.
import type { Sending } from "./threads.mjs";
export default function (x: Sending<number[]>) { return x; }
function helper() {}

// @filename: cjs.cts
// Emitted as CommonJS: nothing is imported or registered.
import type { Sending } from "./threads.mjs";
export function c(x: Sending<number[]>) { return x; }

// @filename: script.ts
// A script cannot import the runtime: nothing is registered.
function scriptFunction() { return [1]; }
