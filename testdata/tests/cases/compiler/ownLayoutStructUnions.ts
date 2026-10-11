// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// Unions of shared structs with one layout: subtype reduction keeps the structs of a union apart,
// and a field at the same place in every struct of the union is read and written in place.

// @filename: threads.d.mts
// The shared struct part of threads.d.mts (porffor's threads/escape-proto).
declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;

/** A cell: shared and mutable. Any number of threads may hold the same one. */
export interface Cell {
  readonly [cellBrand]: true;
}

/** A field of a shared struct, declared with `f64` or `ref<T>()`. */
export interface Field<T> {
  readonly [fieldBrand]: T;
}
/** A number field: an f64 in shared memory. */
export declare const f64: Field<number>;
/** A reference field: a shared struct or a SharedArray, or null. */
export declare function ref<T>(): Field<T | null>;

/** What `struct(...)` returns: `new S()` makes a zeroed struct in shared memory. */
export interface StructType<T> {
  new (): T & Cell;
  readonly fields: readonly string[];
}
/** The value type of a struct type: `type Body = Struct<typeof Body>`. */
export type Struct<S> = S extends StructType<infer T> ? T & Cell : never;

export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;
export declare function struct<T>(fields: { [K in keyof T]-?: Field<T[K]> }): StructType<T>;

/** Prints a line, synchronously, from any thread. */
export declare function log(...args: unknown[]): void;

// @filename: same.mts
import { f64, log, ref, struct, type Struct } from "./threads.mjs";

const A = struct({ u: f64 });
type A = Struct<typeof A>;
const A2 = struct({ u: f64 });
type A2 = Struct<typeof A2>;
const flag = Math.random() > 2;

// Through `new`: a struct type that is one of two with the same layout.
const Either = flag ? struct({ u: f64 }) : A;
export function viaNew() {
    const m = new Either();                 // one of two structs with one layout
    m.u = 7;                                // slot 1 of either
    log(`${m.u}`);
}

export function viaNewOfUnion() {
    const m = flag ? new A() : new A2();    // A | A2
    return m.u;
}

// Through a parameter: a union of structs, or of struct types.
export function viaParam(x: A | A2 | null) {
    if (x !== null) x.u += 1;
    return x === null ? 0 : x.u;
}

export function viaStructTypeParam(S: typeof A | typeof A2) {
    const m = new S();
    m.u = 2;
    return m.u;
}

// Structs that differ only after a field have it at the same place.
const P = struct({ u: f64, w: f64 });
type P = Struct<typeof P>;
export function prefix(x: A | P) {
    return x.u;
}

// So do reference fields.
const R = struct({ next: ref<A>(), u: f64 });
type R = Struct<typeof R>;
const R2 = struct({ next: ref<A>(), x: f64, u: f64 });
type R2 = Struct<typeof R2>;
export function references(r: R | R2) {
    r.next = new A();
    return r.next;
}
