// @target: es2022
// @module: esnext
// @strict: true
// @lib: es2022

// Without `ownRuntime`, a shared struct calls a free `__sharedStruct` and nothing is imported or
// registered.

// @filename: own.d.mts
export declare function share<T>(x: sending T): shared T;

// @filename: main.mts
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

// @filename: structOnly.mts
export shared struct Only { v: number }

// @filename: functionsOnly.mts
export function f(x: sending number[]) { return x; }
function g() {}

// @filename: plain.mts
export function plain() { return 1; }

// @filename: collision.mts
export function __register(x: sending number[]) { return x; }
export shared struct S { v: number }
function __sharedStruct() {}

// @filename: nameless.mts
export default function (x: sending number[]) { return x; }
function helper() {}

// @filename: cjs.cts
export function c(x: sending number[]) { return x; }

// @filename: script.ts
declare function send<T>(x: sending T): void;
function scriptFunction() { send([1]); }
