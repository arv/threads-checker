//// [tests/cases/compiler/ownLayoutStructUnions.ts] ////

//// [threads.d.mts]
// The shared struct part of threads.d.mts (now threads.d.ts in github.com/arv/threads).
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

//// [same.mts]
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


//// [same.mjs]
import { F64 as __F64, I32 as __I32, newStruct as __newStruct, register as __register, release as __release, retain as __retain, sharedStruct as __sharedStruct, store as __store } from "./threads.mjs";
import { f64, log, ref, struct } from "./threads.mjs";
const A = __sharedStruct("A", ["u"], []);
const A2 = __sharedStruct("A2", ["u"], []);
const flag = Math.random() > 2;
// Through `new`: a struct type that is one of two with the same layout.
const Either = flag ? __sharedStruct("Struct", ["u"], []) : A;
export function viaNew() {
    const m = __newStruct(Either); // one of two structs with one layout
    try {
        __F64[(m >> 3) + 1] = 7; // slot 1 of either
        log(`${__F64[(m >> 3) + 1]}`);
    }
    finally {
        __release(m);
    }
}
export function viaNewOfUnion() {
    const m = flag ? __newStruct(A) : __newStruct(A2); // A | A2
    try {
        return __F64[(m >> 3) + 1];
    }
    finally {
        __release(m);
    }
}
// Through a parameter: a union of structs, or of struct types.
export function viaParam(x) {
    if (x !== 0)
        __F64[(x >> 3) + 1] += 1;
    return x === 0 ? 0 : __F64[(x >> 3) + 1];
}
export function viaStructTypeParam(S) {
    const m = __newStruct(S);
    try {
        __F64[(m >> 3) + 1] = 2;
        return __F64[(m >> 3) + 1];
    }
    finally {
        __release(m);
    }
}
// Structs that differ only after a field have it at the same place.
const P = __sharedStruct("P", ["u", "w"], []);
export function prefix(x) {
    return __F64[(x >> 3) + 1];
}
// So do reference fields.
const R = __sharedStruct("R", ["next", "u"], [0]);
const R2 = __sharedStruct("R2", ["next", "x", "u"], [0]);
export function references(r) {
    __store((r >> 2) + 2, __newStruct(A));
    return __retain(__I32[(r >> 2) + 2]);
}
__register(import.meta.url, { viaNew, viaNewOfUnion, viaParam, viaStructTypeParam, prefix, references });
