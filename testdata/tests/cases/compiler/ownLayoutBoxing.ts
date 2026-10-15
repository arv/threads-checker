// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// Boxing: a shared struct or SharedArray value is boxed (`__box`) where it flows into a place typed
// otherwise, and unboxed (`__unbox`) where such a value flows into a struct-typed place.


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

// @filename: boxing.mts
import { f64, log, SharedArray, struct, type Struct } from "./threads.mjs";

export const Vec = struct({ x: f64, y: f64 });
export type Vec = Struct<typeof Vec>;

declare function takeVec(v: Vec): void;
declare function takeUnknown(u: unknown): void;
declare function identity<T>(x: T): T;
declare const anything: any;

export function roundTrip(v: Vec) {
    const u: unknown = v;                    // box
    const v2 = u as Vec;                     // unbox
    return v2.x;
}

export function boxed(v: Vec, other: unknown, a: SharedArray<number>) {
    log(v, a);                               // host function arguments
    takeUnknown(v);
    const text = `${v} and ` + v;            // strings
    const kind = typeof v;
    const same = v === other;                // equality with a non-struct value
    const isNull = v === null;               // stays raw
    const union: Vec | number = v;
    const object: object = v;
    const list: unknown[] = [v, a];
    const record: { v: unknown } = { v };
    const copy = { ...v };
    const s = v.toString();                  // not a field
    const { x } = v;                         // destructured through the handle
    const has = "x" in v;
    const same2 = identity(v);               // generic: decided by the instantiation, stays raw
    return [text, kind, same, isNull, union, object, list, record, copy, s, x, has, same2.y];
}

export function unboxed(u: unknown, a: any) {
    takeVec(anything);                       // any into a struct parameter
    const v = u as Vec;
    const w: Vec = a;
    if (isVec(u)) {
        u.x = 1;                             // narrowed: unboxed where the layout is used
        takeVec(u);
    }
    return v.y + w.y;
}

function isVec(u: unknown): u is Vec {
    return u !== null && typeof u === "object" && "x" in u;
}

export function returns(v: Vec): unknown {
    return v;                                // box
}

export function returnsVec(u: unknown): Vec {
    return u as Vec;                         // unbox (the assertion)
}

// @filename: assertion.ts
import type { Vec } from "./boxing.mjs";
export function angleBracket(u: unknown) {
    return (<Vec>u).x;                       // unbox (angle-bracket assertion)
}
