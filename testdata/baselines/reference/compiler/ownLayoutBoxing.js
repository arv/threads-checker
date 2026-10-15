//// [tests/cases/compiler/ownLayoutBoxing.ts] ////

//// [threads.d.mts]
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

//// [boxing.mts]
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

//// [assertion.ts]
import type { Vec } from "./boxing.mjs";
export function angleBracket(u: unknown) {
    return (<Vec>u).x;                       // unbox (angle-bracket assertion)
}


//// [boxing.mjs]
import { F64 as __F64, box as __box, register as __register, release as __release, retain as __retain, sharedStruct as __sharedStruct, unbox as __unbox } from "./threads.mjs";
import { f64, log, struct } from "./threads.mjs";
export const Vec = __sharedStruct("Vec", ["x", "y"], []);
export function roundTrip(v) {
    const u = __box(v); // box
    const v2 = __unbox(u); // unbox
    return __F64[(v2 >> 3) + 1];
}
export function boxed(v, other, a) {
    log(__box(v), __box(a)); // host function arguments
    takeUnknown(__box(v));
    const text = `${__box(v)} and ` + __box(v); // strings
    const kind = typeof __box(v);
    const same = __box(v) === other; // equality with a non-struct value
    const isNull = v === 0; // stays raw
    const union = __box(v);
    const object = __box(v);
    const list = [__box(v), __box(a)];
    const record = { v: __box(v) };
    const copy = { ...__box(v) };
    const s = __box(v).toString(); // not a field
    const { x } = __box(v); // destructured through the handle
    const has = "x" in __box(v);
    const same2 = __retain(identity(v)); // generic: decided by the instantiation, stays raw
    try {
        return [text, kind, same, isNull, union, object, list, record, copy, s, x, has, __F64[(same2 >> 3) + 2]];
    }
    finally {
        __release(same2);
    }
}
export function unboxed(u, a) {
    takeVec(__unbox(anything)); // any into a struct parameter
    const v = __unbox(u);
    const w = __unbox(a);
    if (isVec(u)) {
        __F64[(__unbox(u) >> 3) + 1] = 1; // narrowed: unboxed where the layout is used
        takeVec(__unbox(u));
    }
    return __F64[(v >> 3) + 2] + __F64[(w >> 3) + 2];
}
function isVec(u) {
    return u !== null && typeof u === "object" && "x" in u;
}
export function returns(v) {
    return __box(v); // box
}
export function returnsVec(u) {
    return __retain(__unbox(u)); // unbox (the assertion)
}
__register(import.meta.url, { roundTrip, boxed, unboxed, isVec, returns, returnsVec });
//// [assertion.js]
import { F64 as __F64, unbox as __unbox } from "./threads.mjs";
export function angleBracket(u) {
    return __F64[(__unbox(u) >> 3) + 1]; // unbox (angle-bracket assertion)
}
