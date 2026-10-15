// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// Typed layouts of shared structs and SharedArrays with `ownRuntime`: field and element accesses
// become typed-array accesses on the shared heap.


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

// @filename: layout.mts
import { f64, ref, SharedArray, struct, type Struct } from "./threads.mjs";

export const Vec = struct({ x: f64, y: f64, z: f64 });
export type Vec = Struct<typeof Vec>;
export const Particle = struct({ pos: ref<Vec>(), mass: f64, trail: ref<SharedArray<number>>() });
export type Particle = Struct<typeof Particle>;

declare function make(): Vec;
declare function pick(): number;
declare function arrays(): SharedArray<Vec>;

export function fields(v: Vec, p: Particle) {
    const x = v.x;                          // read: slot 1
    v.z = x + v.y;                          // write
    v.x += 1;                               // compound assignment
    v.y++;                                  // postfix
    --v.z;                                  // prefix
    const m = make().y;                     // object with side effects: no temporary needed
    p.pos!.x = p.mass;                      // chain through a struct-typed field
    return v === p.pos;                     // addresses compare as numbers
}

export function elements(vs: SharedArray<Vec>, i: number) {
    const v = vs[i];                        // element address, bounds-checked
    vs[i].x = vs[i + 1].y;                  // chains; index not simple: temporary
    const n = vs.length;                    // header word
    const w = arrays()[pick()];             // array and index not simple: temporaries
    return n + w.z + v.x;
}

export function numbers(a: SharedArray<number>, p: Particle, i: number) {
    const first = a[0];                     // read
    a[i] = first * 2;                       // write
    a[i + 1] += 3;                          // compound assignment, index not simple
    a[i]++;
    p.trail![i] = a.length;                 // array from a field: temporary
    return a[i];
}

export function allocate(n: number) {
    const v = new Vec();
    const vs = new SharedArray(Vec, n);
    const ns = new SharedArray(Number, n);
    ns[0] = v.x;
    return vs;
}
