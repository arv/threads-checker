// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./own.mjs
// @noEmit: true

// Sending a counted value on a channel moves it: the sender must not use it, or what aliases it,
// again. A thread's arguments share it. Lines marked "error" are expected to be reported.

// @filename: own.d.mts
// Types for the own/shared threads runtime own.mjs (now own.ts in github.com/arv/threads). Programs
// import their API from "./own.mjs"; the compiler adds the imports of the helpers it emits
// (`__sharedStruct`, `__register`) when `ownRuntime` is set.
//
// `sending T` marks a value whose ownership moves to the receiver: the sender must not use it
// afterwards. `shared T` marks a value any number of threads may read at once: deeply immutable, or
// a cell that synchronizes internally (Slots, Barrier, a shared struct).

/** One slot per thread: a thread publishes into its own slot, and everyone reads every slot. */
export interface SlotsCell<T> {
    readonly length: number;
    /** Publishes `value` in slot `index`, replacing what was there. */
    set(index: number, value: T): void;
    /** The value in slot `index`. */
    get(index: number): shared T;
    /** The arrays in all slots, concatenated in slot order. */
    flat(): shared (T extends readonly (infer E)[] ? E : T)[];
}
export declare const Slots: new <T>(count: number) => shared SlotsCell<T>;

/** A reusable barrier for a fixed number of threads. */
export interface BarrierCell {
    /** Blocks until all threads have called `wait`. */
    wait(): void;
}
export declare const Barrier: new (threads: number) => shared BarrierCell;

/** Freezes a value the caller owns into a deeply immutable value every thread can read. */
export declare function share<T>(value: sending T): shared T;

/** A thread started inside a `scope`; joining it moves the thread's result back. */
export interface Thread<R> {
    join(): sending R;
}

/** Spawns threads that cannot outlive the scope. Every argument is sent to the new thread. */
export interface Scope {
    spawn<A extends unknown[], R>(f: (...args: A) => sending R, ...args: sending A): shared Thread<R>;
}

/** Runs `body` and joins every thread it spawned before returning. */
export declare function scope<R>(body: (s: shared Scope) => R): R;

/** Splits an owned array into `parts` owned, disjoint chunks of nearly equal size. Trusted. */
export declare function partition<T>(items: sending T[], parts: number): (sending T[])[];

/** Takes element `index` out of an array of owned values, so the caller owns it. Trusted. */
export declare function take<T>(items: (sending T)[], index: number): sending T;

/** Prints a line, synchronously, from any thread. */
export declare function log(...args: unknown[]): void;

/** A shared struct's value as a type: what `new SharedArray(Struct, n)` takes. */
export interface SharedStructType<T> {
    new (): T;
}

/**
 * A fixed-length array in the shared heap, of numbers or of shared structs stored inline. Threads
 * share it with no copying. With `ownRuntime` the compiler lays it out in the shared heap and turns
 * `a[i]` and `a.length` into typed-array accesses.
 */
export interface SharedArray<T> {
    readonly length: number;
    [index: number]: T;
}
export declare const SharedArray: {
    new (type: NumberConstructor, length: number): SharedArray<number>;
    new <T>(type: SharedStructType<T>, length: number): SharedArray<T>;
};


// @filename: chan.d.mts
export interface ChannelCell<T> {
    send(value: sending T): void;
    recv(): sending T | undefined;
    close(): void;
    [Symbol.iterator](): Iterator<T>;
}
export declare const Channel: new <T>(capacity?: number) => shared ChannelCell<T>;
export declare function go<A extends unknown[]>(f: (...args: A) => void, ...args: sending A): void;
export declare function select<T>(...channels: (shared ChannelCell<T>)[]): [index: number, value: T | undefined];

// @filename: moves.mts
import { SharedArray } from "./own.mjs";
import { type ChannelCell, go, select } from "./chan.mjs";

export shared struct Node { value: number; next: Node | null }

declare const flag: boolean;

export function useAfterSend(ch: shared ChannelCell<Node>) {
    const n = new Node();
    ch.send(n);
    return n.value;                          // error: sent
}

export function alias(ch: shared ChannelCell<Node>) {
    const n = new Node();
    const other = n;
    ch.send(n);
    return other.value;                      // error: shares the block with n
}

export function element(ch: shared ChannelCell<SharedArray<Node>>) {
    const nodes = new SharedArray(Node, 4);
    const first = nodes[0];                  // stored inline in the array's block
    ch.send(nodes);
    return first.value;                      // error
}

export function maybe(ch: shared ChannelCell<Node>) {
    const n = new Node();
    if (flag) ch.send(n);
    return n.value;                          // error: may have been sent
}

export function twice(ch: shared ChannelCell<Node>) {
    const n = new Node();
    ch.send(n);
    ch.send(n);                              // error: sent
}

export function borrowed(ch: shared ChannelCell<Node>, n: Node) {
    ch.send(n);                              // error: the caller may still use it
}

export function owned(ch: shared ChannelCell<Node>, n: sending Node) {
    ch.send(n);                              // fine: declared `sending`
}

export function captured(ch: shared ChannelCell<Node>) {
    const n = new Node();
    return () => ch.send(n);                 // error: captured from an outer scope
}

export function inLoop(ch: shared ChannelCell<Node>) {
    for (let i = 0; i < 3; i++) {
        const n = new Node();                // fresh in every iteration
        ch.send(n);
    }
    const kept = new Node();
    for (let i = 0; i < 3; i++) ch.send(kept);   // error: sent in an earlier iteration
}

// Not moves: a thread's arguments share a counted value, and a reference field is not tracked.
function work(n: Node) {}
export function notMoves(ch: shared ChannelCell<Node>, holder: Node) {
    const n = new Node();
    go(work, n);
    go(work, n);
    ch.send(holder.next!);
    return n.value + holder.next!.value;     // not reported: aliases through reference fields are not tracked
}

// select returns its value in an ordinary tuple, where nothing could count it.
export function selects(a: shared ChannelCell<Node>, b: shared ChannelCell<Node>) {
    const result = select(a, b);             // error
    return result[0];
}

// A for-of over a channel owns its values; over an ordinary array, it cannot.
export function loops(ch: shared ChannelCell<Node>, list: Node[]) {
    let total = 0;
    for (const n of ch) total += n.value;
    for (const n of list) total += n.value;  // error
    return total;
}
