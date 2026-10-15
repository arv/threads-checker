// @target: es2022
// @module: commonjs
// @ownRuntime: ./threads.mjs

// `ownRuntime` needs ES module output: with CommonJS it is an error, and its emit is not done
// (no helper imports, no struct layouts, no registration).

// @filename: threads.d.mts
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

// @filename: main.ts
import { f64, type Sending, struct } from "./threads.mjs";

export const S = struct({ v: f64 });
export function f(x: Sending<number[]>) { return x; }
