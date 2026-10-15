// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// Bounds-check elimination for SharedArray element accesses: the check is dropped only where the
// index provably counts up from a non-negative integer in a loop bounded by the same array's length.


// @filename: threads.d.mts
// The parts of the threads runtime (threads.mjs, from github.com/arv/threads) this test uses.
// Programs import their API from "./threads.mjs"; the compiler adds the imports of the helpers it
// emits (`__sharedStruct`, `__register`, ...) when `ownRuntime` is set.

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

/** Declares a shared struct: fields at fixed places in shared memory. */
export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;
export declare function struct<T>(fields: { [K in keyof T]-?: Field<T[K]> }): StructType<T>;

/**
 * A fixed-length array in shared memory, of numbers or of shared structs stored inline. With
 * `ownRuntime` the compiler turns `a[i]` and `a.length` into typed-array accesses.
 */
export interface SharedArray<T> extends Cell {
  readonly length: number;
  [index: number]: T;
}
export declare const SharedArray: {
  new (type: NumberConstructor, length: number): SharedArray<number>;
  new <T>(type: StructType<T>, length: number): SharedArray<T & Cell>;
};

/** Prints a line, synchronously, from any thread. */
export declare function log(...args: unknown[]): void;

// @filename: bounds.mts
import { f64, SharedArray, struct, type Struct } from "./threads.mjs";

export const P = struct({ x: f64, y: f64 });
export type P = Struct<typeof P>;

const START = 1;

export function eliminated(a: SharedArray<number>, ps: SharedArray<P>) {
    let sum = 0;
    for (let i = 0; i < a.length; i++) sum += a[i];                      // bound a.length
    const n = a.length;
    for (let i = 0; i < n; ++i) a[i] = sum;                             // bound const n = a.length, ++i
    for (let i = START; i < a.length; i += 1) a[i] += 1;                // init from a const literal, i += 1
    for (let i = 0; i < ps.length; i++) {
        ps[i].x = ps[i].y;                                              // struct elements
        for (let j = 0; j < ps.length; j++) sum += ps[j].x - ps[i].x;   // nested loops
    }
    const local = new SharedArray(Number, 4);
    for (let i = 0; i < local.length; i++) local[i]++;                  // a const array
    return sum;
}

export function kept(a: SharedArray<number>, b: SharedArray<number>, lo: number) {
    let sum = 0;
    for (let i = lo; i < a.length; i++) sum += a[i];                     // init not a literal
    for (let i = -1; i < a.length; i++) sum += a[i];                     // negative init
    for (let i = 0; i < b.length; i++) sum += a[i];                      // bound from another array
    for (let i = 0; i < a.length; i++) { i = 5; sum += a[i]; }           // i assigned in the body
    for (let i = 0; i < a.length; i++) { const f = () => { i++; }; f(); sum += a[i]; } // i written in a closure
    for (let i = 0; i < a.length; i += 2) sum += a[i];                   // update by 2
    let m = a.length;
    m = 100;
    for (let i = 0; i < m; i++) sum += a[i];                             // bound is a reassigned let
    for (let i = 0; i <= a.length; i++) sum += a[i];                     // <= instead of <
    for (var i = 0; i < a.length; i++) sum += a[i];                      // var, not let
    for (let k = 0; k < a.length; k++) [0].forEach(() => { sum += a[k]; }); // inside a closure
    return sum;
}

export function reassigned(a: SharedArray<number>, b: SharedArray<number>) {
    let c = a;
    let sum = 0;
    for (let i = 0; i < c.length; i++) sum += c[i];                      // c is reassigned below
    c = b;
    for (let i = 0; i < a.length; i++) a[i] = sum;                       // a itself is never assigned: eliminated
    return sum;
}
