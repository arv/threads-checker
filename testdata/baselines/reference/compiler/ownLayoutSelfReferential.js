//// [tests/cases/compiler/ownLayoutSelfReferential.ts] ////

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
//// [tree.mts]
import { f64, ref, SharedArray, struct } from "./threads.mjs";

export interface Tree { value: number; children: SharedArray<Tree> | null }
export const Tree = struct<Tree>({ value: f64, children: ref<SharedArray<Tree>>() });

export function grow(t: Tree, n: number) {
    t.children = new SharedArray(Tree, n);  // field write: an address
    t.children[0].value = t.value + 1;      // element of the struct's own array
    return t.children.length;
}

export function sum(t: Tree): number {
    let total = t.value;
    const children = t.children;
    if (children) {
        for (let i = 0; i < children.length; i++) {
            total += sum(children[i]);
        }
    }
    return total;
}


//// [tree.mjs]
import { F64 as __F64, I32 as __I32, newSharedArray as __newSharedArray, outOfBounds as __outOfBounds, register as __register, release as __release, retain as __retain, sharedStruct as __sharedStruct, store as __store } from "./threads.mjs";
import { f64, ref, SharedArray, struct } from "./threads.mjs";
export const Tree = __sharedStruct("Tree", ["value", "children"], [1]);
export function grow(t, n) {
    var _a;
    __store((t >> 2) + 4, __newSharedArray(Tree, n)); // field write: an address
    __F64[((_a = __I32[(t >> 2) + 4], (0 >>> 0) === 0 && 0 < __I32[(_a + 4) >> 2] ? _a + 16 + 0 * 24 : __outOfBounds[0]) >> 3) + 1] = __F64[(t >> 3) + 1] + 1; // element of the struct's own array
    return __I32[(__I32[(t >> 2) + 4] + 4) >> 2];
}
export function sum(t) {
    let total = __F64[(t >> 3) + 1];
    const children = __retain(__I32[(t >> 2) + 4]);
    try {
        if (children) {
            for (let i = 0; i < __I32[(children + 4) >> 2]; i++) {
                total += sum((i >>> 0) === i && i < __I32[(children + 4) >> 2] ? children + 16 + i * 24 : __outOfBounds[i]);
            }
        }
        return total;
    }
    finally {
        __release(children);
    }
}
__register(import.meta.url, { grow, sum });
