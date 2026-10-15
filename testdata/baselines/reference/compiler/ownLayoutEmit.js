//// [tests/cases/compiler/ownLayoutEmit.ts] ////

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

//// [layout.mts]
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


//// [layout.mjs]
import { F64 as __F64, I32 as __I32, newSharedArray as __newSharedArray, newStruct as __newStruct, outOfBounds as __outOfBounds, register as __register, release as __release, retain as __retain, sharedStruct as __sharedStruct } from "./threads.mjs";
import { f64, ref, SharedArray, struct } from "./threads.mjs";
export const Vec = __sharedStruct("Vec", ["x", "y", "z"], []);
export const Particle = __sharedStruct("Particle", ["pos", "mass", "trail"], [0, 2]);
export function fields(v, p) {
    const x = __F64[(v >> 3) + 1]; // read: slot 1
    __F64[(v >> 3) + 3] = x + __F64[(v >> 3) + 2]; // write
    __F64[(v >> 3) + 1] += 1; // compound assignment
    __F64[(v >> 3) + 2]++; // postfix
    --__F64[(v >> 3) + 3]; // prefix
    const m = __F64[(make() >> 3) + 2]; // object with side effects: no temporary needed
    __F64[(__I32[(p >> 2) + 2] >> 3) + 1] = __F64[(p >> 3) + 2]; // chain through a struct-typed field
    return v === __I32[(p >> 2) + 2]; // addresses compare as numbers
}
export function elements(vs, i) {
    var _a, _b, _c, _d;
    const v = (i >>> 0) === i && i < __I32[(vs + 4) >> 2] ? vs + 16 + i * 32 : __outOfBounds[i]; // element address, bounds-checked
    __F64[(((i >>> 0) === i && i < __I32[(vs + 4) >> 2] ? vs + 16 + i * 32 : __outOfBounds[i]) >> 3) + 1] = __F64[((_a = i + 1, (_a >>> 0) === _a && _a < __I32[(vs + 4) >> 2] ? vs + 16 + _a * 32 : __outOfBounds[_a]) >> 3) + 2]; // chains; index not simple: temporary
    const n = __I32[(vs + 4) >> 2]; // header word
    const w = (_d = __retain((_b = __retain(arrays()), _c = pick(), (_c >>> 0) === _c && _c < __I32[(_b + 4) >> 2] ? _b + 16 + _c * 32 : __outOfBounds[_c])), __release(_b), _d); // array and index not simple: temporaries
    try {
        return n + __F64[(w >> 3) + 3] + __F64[(v >> 3) + 1];
    }
    finally {
        __release(w);
    }
}
export function numbers(a, p, i) {
    var _a, _b;
    const first = (0 >>> 0) === 0 && 0 < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + 0] : __outOfBounds[0]; // read
    __F64[((a + 16) >> 3) + ((i >>> 0) === i && i < __I32[(a + 4) >> 2] ? i : __outOfBounds[i])] = first * 2; // write
    __F64[_a = i + 1, ((a + 16) >> 3) + ((_a >>> 0) === _a && _a < __I32[(a + 4) >> 2] ? _a : __outOfBounds[_a])] += 3; // compound assignment, index not simple
    __F64[((a + 16) >> 3) + ((i >>> 0) === i && i < __I32[(a + 4) >> 2] ? i : __outOfBounds[i])]++;
    __F64[_b = __I32[(p >> 2) + 6], ((_b + 16) >> 3) + ((i >>> 0) === i && i < __I32[(_b + 4) >> 2] ? i : __outOfBounds[i])] = __I32[(a + 4) >> 2]; // array from a field: temporary
    return (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __outOfBounds[i];
}
export function allocate(n) {
    var _a;
    const v = __newStruct(Vec);
    try {
        let vs = __newSharedArray(Vec, n);
        try {
            const ns = __newSharedArray(8, n);
            try {
                __F64[((ns + 16) >> 3) + ((0 >>> 0) === 0 && 0 < __I32[(ns + 4) >> 2] ? 0 : __outOfBounds[0])] = __F64[(v >> 3) + 1];
                return (_a = vs, vs = 0, _a);
            }
            finally {
                __release(ns);
            }
        }
        finally {
            __release(vs);
        }
    }
    finally {
        __release(v);
    }
}
__register(import.meta.url, { fields, elements, numbers, allocate });
