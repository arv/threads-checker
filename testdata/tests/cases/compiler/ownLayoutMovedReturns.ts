// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// A counted value that a callback returns to the runtime, whose function type has a `Sending<T>`
// result (a SharedMap's getOrInsertComputed's callback), is passed as `__moved(E)`: it carries its
// count, which the runtime takes over, and the number is an address. Another callback's result is
// not.

// @filename: threads.d.mts
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

// @filename: main.mts
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
