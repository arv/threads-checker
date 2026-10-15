// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs
// @noEmit: true

// Sending a counted value on a channel moves it: the sender must not use it, or what aliases it,
// again. A thread's arguments share it. Lines marked "error" are expected to be reported.

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

// @filename: moves.mts
import { type Channel, f64, go, ref, select, SharedArray, type Sending, struct } from "./threads.mjs";

export interface Node { value: number; next: Node | null }
export const Node = struct<Node>({ value: f64, next: ref<Node>() });

declare const flag: boolean;

export function useAfterSend(ch: Channel<Node>) {
    const n = new Node();
    ch.send(n);
    return n.value;                          // error: sent
}

export function alias(ch: Channel<Node>) {
    const n = new Node();
    const other = n;
    ch.send(n);
    return other.value;                      // error: shares the block with n
}

export function element(ch: Channel<SharedArray<Node>>) {
    const nodes = new SharedArray(Node, 4);
    const first = nodes[0];                  // stored inline in the array's block
    ch.send(nodes);
    return first.value;                      // error
}

export function maybe(ch: Channel<Node>) {
    const n = new Node();
    if (flag) ch.send(n);
    return n.value;                          // error: may have been sent
}

export function twice(ch: Channel<Node>) {
    const n = new Node();
    ch.send(n);
    ch.send(n);                              // error: sent
}

export function borrowed(ch: Channel<Node>, n: Node) {
    ch.send(n);                              // error: the caller may still use it
}

export function owned(ch: Channel<Node>, n: Sending<Node>) {
    ch.send(n);                              // fine: declared `Sending<Node>`
}

export function captured(ch: Channel<Node>) {
    const n = new Node();
    return () => ch.send(n);                 // error: captured from an outer scope
}

export function inLoop(ch: Channel<Node>) {
    for (let i = 0; i < 3; i++) {
        const n = new Node();                // fresh in every iteration
        ch.send(n);
    }
    const kept = new Node();
    for (let i = 0; i < 3; i++) ch.send(kept);   // error: sent in an earlier iteration
}

// Not moves: a thread's arguments share a counted value, and a reference field is not tracked.
function work(n: Node) {}
export function notMoves(ch: Channel<Node>, holder: Node) {
    const n = new Node();
    go(work, n);
    go(work, n);
    ch.send(holder.next!);
    return n.value + holder.next!.value;     // not reported: aliases through reference fields are not tracked
}

// select returns its value in an ordinary tuple, where nothing could count it.
export function selects(a: Channel<Node>, b: Channel<Node>) {
    const result = select(a, b);             // error
    return result[0];
}

// A for-of over a channel owns its values; over an ordinary array, it cannot.
export function loops(ch: Channel<Node>, list: Node[]) {
    let total = 0;
    for (const n of ch) total += n.value;
    for (const n of list) total += n.value;  // error
    return total;
}
