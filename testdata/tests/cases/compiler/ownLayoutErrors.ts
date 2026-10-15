// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs
// @noEmit: true

// Errors of the typed layouts of shared structs and SharedArrays.


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

// @filename: errors.mts
import { f64, ref, SharedArray, struct, type Struct } from "./threads.mjs";

export const Item = struct({ v: f64 });
export type Item = Struct<typeof Item>;
export interface Good { a: number; b: Good | null; c: SharedArray<number> | null; d: SharedArray<Item> | null }
export const Good = struct<Good>({ a: f64, b: ref<Good>(), c: ref<SharedArray<number>>(), d: ref<SharedArray<Item>>() });
// Bad's field types come from its interface (`as never` gets the fields past TypeScript).
export interface Bad {
    s: string | null;
    f: boolean | null;
    o?: number;
    u: number | undefined;
    arr: number[];
    obj: { x: number };
    t: SharedArray<bigint>;
}
export const Bad = struct<Bad>({ s: f64 as never, f: f64 as never, o: f64, u: f64, arr: f64 as never, obj: f64 as never, t: f64 as never });

export function elements(a: SharedArray<Good>, g: Good, i: number, maybe: Good | undefined) {
    a[i] = g;                                // elements are inline
    const n = maybe?.a;                      // optional chaining
    const bad = new SharedArray<bigint>(Good as any, 3);
    return n;
}

export function destructure({ a }: Good, list: Good[]) {
    for (const { b } of list) {}
    return a;
}
