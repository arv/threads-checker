// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// Counted values (shared structs and SharedArrays) through channels: a thread's arguments and a
// message take a count of their own (`__ref(E)`), and a received value is owned (+1).

// @filename: threads.d.mts
// The parts of the threads runtime's plain TypeScript API (threads.d.ts in github.com/arv/threads)
// that this test uses. Programs import their API from "./threads.mjs"; the compiler adds the imports
// of the helpers it emits (`__sharedStruct`, `__register`, ...) when `ownRuntime` is set.

declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;

/** A cell: shared and mutable. Any number of threads may hold the same one. */
export interface Cell {
  readonly [cellBrand]: true;
}

/** A value whose ownership moves, on a parameter or a result. To TypeScript it is just T. */
export type Sending<T> = T;

/** A deeply immutable value, which any number of threads may read. */
export type Shared<T> = T extends Cell | ((...args: any[]) => any) ? T
  : T extends object ? { readonly [K in keyof T]: Shared<T[K]> }
  : T;

/** A field of a shared struct. */
export interface Field<T> {
  readonly [fieldBrand]: T;
}
/** A number field: an f64 in shared memory. */
export declare const f64: Field<number>;
/** A reference field: a shared struct, a SharedArray or a Channel, or null. */
export declare function ref<T>(): Field<T | null>;

/** What `struct(...)` returns: `new S()` makes a zeroed struct in shared memory. */
export interface StructType<T> {
  new (): T & Cell;
  readonly fields: readonly string[];
}
/** The value type of a struct type: `type Body = Struct<typeof Body>`. */
export type Struct<S> = S extends StructType<infer T> ? T & Cell : never;

/** Declares a shared struct; one that refers to its own type is declared with an interface. */
export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;
export declare function struct<T>(fields: { [K in keyof T]-?: Field<T[K]> }): StructType<T>;

/** A fixed-length array in shared memory, of numbers or of shared structs stored inline. */
export interface SharedArray<T> extends Cell {
  readonly length: number;
  [index: number]: T;
}
export declare const SharedArray: {
  new (type: NumberConstructor, length: number): SharedArray<number>;
  new <T>(type: StructType<T>, length: number): SharedArray<T & Cell>;
};

/** A channel: values move through it from thread to thread, in order. */
export declare class Channel<T> implements Cell {
  readonly [cellBrand]: true;
  /** Holds up to `capacity` values; 0, the default, is unbuffered. */
  constructor(capacity?: number);
  /** Moves `value` in. */
  send(value: Sending<T>): void;
  /** The next value, which the caller now owns; undefined once closed and empty. */
  recv(): Sending<T> | undefined;
  /** No more sends. */
  close(): void;
  [Symbol.iterator](): Iterator<T>;
}
/** Starts `f` on a thread of its own, a goroutine. Every argument is sent. */
export declare function go<A extends unknown[]>(f: (...args: A) => void, ...args: Sending<A>): void;
/** Waits until one of the channels has a value or is closed: its index and the value. */
export declare function select<T>(...channels: Channel<T>[]): [index: number, value: T | undefined];

// @filename: channels.mts
import { Channel, f64, go, ref, SharedArray, type Sending, struct, type Struct } from "./threads.mjs";

export interface Node { value: number; next: Node | null }
export const Node = struct<Node>({ value: f64, next: ref<Node>() });
export const Box = struct({ item: ref<Node>() });
export type Box = Struct<typeof Box>;

// Sends: the message takes a count of its own, and the sender's count is released as usual.
export function sends(ch: Channel<Node>, box: Box, mine: Sending<Node>) {
    const n = new Node();
    ch.send(n);                              // an owned local
    ch.send(new Node());                     // an owned temporary, released after the statement
    ch.send(box.item!);                      // a reference field: retained by `__ref` at once
    ch.send(mine);                           // a parameter declared `Sending<Node>`
}

// A thread's arguments share a counted value: the same array goes to every thread.
export function threads(ch: Channel<Node>, arr: SharedArray<number>) {
    go(worker, arr, ch);
    go(worker, arr, ch);
    return arr.length;
}

// A for-of over a channel owns each value it receives, until the iteration ends.
function worker(arr: SharedArray<number>, ch: Channel<Node>) {
    for (const n of ch) arr[0] += n.value;
}

// recv() hands over a count: an owned local, a temporary, or a discarded value released at once.
export function receives(ch: Channel<Node>): Node | null {
    const a = ch.recv()!;
    ch.recv();
    const v = a.value + ch.recv()!.value;
    for (const n of ch) {
        if (n.value === v) return n;         // moves the loop variable's count out
    }
    return ch.recv() ?? null;
}

// A stage of a pipeline: what it receives, it sends on (moved).
export function stage(input: Channel<SharedArray<number>>, output: Channel<SharedArray<number>>) {
    for (const block of input) {
        block[0]++;
        output.send(block);
    }
    output.close();
}

export function pipeline(blocks: number) {
    const input = new Channel<SharedArray<number>>(4), output = new Channel<SharedArray<number>>(4);
    go(stage, input, output);
    for (let k = 0; k < blocks; k++) {
        const block = new SharedArray(Number, 16);
        input.send(block);
    }
    input.close();
    let total = 0;
    for (const block of output) total += block[0];
    return total;
}
