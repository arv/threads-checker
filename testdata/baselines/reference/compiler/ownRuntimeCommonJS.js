//// [tests/cases/compiler/ownRuntimeCommonJS.ts] ////

//// [threads.d.mts]
declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;
export interface Cell {
  readonly [cellBrand]: true;
}
export type Sending<T> = T;
export interface Field<T> {
  readonly [fieldBrand]: T;
}
export declare const f64: Field<number>;
export interface StructType<T> {
  new (): T & Cell;
  readonly fields: readonly string[];
}
export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;

//// [main.ts]
import { f64, type Sending, struct } from "./threads.mjs";

export const S = struct({ v: f64 });
export function f(x: Sending<number[]>) { return x; }


//// [main.js]
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.S = void 0;
exports.f = f;
const threads_mjs_1 = require("./threads.mjs");
exports.S = (0, threads_mjs_1.struct)({ v: threads_mjs_1.f64 });
function f(x) { return x; }
