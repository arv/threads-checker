//// [tests/cases/compiler/ownLayoutMovedReturns.ts] ////

//// [threads.d.mts]
// The parts of the threads runtime (threads.mjs, from github.com/arv/threads) this test uses.
declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;

/** A cell: shared and mutable. Any number of threads may hold the same one. */
export interface Cell {
  readonly [cellBrand]: true;
}
/** A value whose ownership moves, on a parameter or a result. To TypeScript it is just T. */
export type Sending<T> = T;

/** A field of a shared struct. */
export interface Field<T> {
  readonly [fieldBrand]: T;
}
/** A number field: an f64 in shared memory. */
export declare const f64: Field<number>;
/** What `struct(...)` returns: `new S()` makes a zeroed struct in shared memory. */
export interface StructType<T> {
  new (): T & Cell;
  readonly fields: readonly string[];
}
/** The value type of a struct type: `type Body = Struct<typeof Body>`. */
export type Struct<S> = S extends StructType<infer T> ? T & Cell : never;
/** Declares a shared struct. */
export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;

/** A map in shared memory. */
export declare class SharedMap<K, V> implements Cell {
  readonly [cellBrand]: true;
  get(key: K): V | undefined;
  set(...entry: Sending<[key: K, value: V]>): this;
  getOrInsertComputed(...entry: Sending<[key: K, callback: (key: K) => Sending<V>]>): V;
}
/** A callback whose result is not sent. */
export declare function peek<V>(f: () => V): void;

//// [main.mts]
import { f64, peek, SharedMap, struct, type Struct } from "./threads.mjs";

const Count = struct({ n: f64 });
type Count = Struct<typeof Count>;

export function count(counts: SharedMap<string, Count>, word: string) {
    counts.getOrInsertComputed(word, () => new Count()).n++;
    counts.getOrInsertComputed(word, function () { return new Count(); }).n++;
    counts.getOrInsertComputed(word, () => {
        const c = new Count();
        c.n = 1;
        return c;
    });
    counts.set(word, new Count());
    peek(() => new Count());
}


//// [main.mjs]
import { F64 as __F64, countedRef as __countedRef, moved as __moved, newStruct as __newStruct, register as __register, release as __release, sharedStruct as __sharedStruct } from "./threads.mjs";
import { f64, peek, struct } from "./threads.mjs";
const Count = __sharedStruct("Count", ["n"], []);
export function count(counts, word) {
    var _a;
    __F64[(counts.getOrInsertComputed(word, () => __moved(__newStruct(Count))) >> 3) + 1]++;
    __F64[(counts.getOrInsertComputed(word, function () { return __moved(__newStruct(Count)); }) >> 3) + 1]++;
    counts.getOrInsertComputed(word, () => {
        var _a;
        let c = __newStruct(Count);
        try {
            __F64[(c >> 3) + 1] = 1;
            return __moved((_a = c, c = 0, _a));
        }
        finally {
            __release(c);
        }
    });
    counts.set(word, __countedRef(_a = __newStruct(Count)));
    __release(_a);
    peek(() => __newStruct(Count));
}
__register(import.meta.url, { count });
