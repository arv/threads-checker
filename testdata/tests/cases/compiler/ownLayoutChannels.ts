// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./own.mjs

// Counted values (shared structs and SharedArrays) through channels: a thread's arguments and a
// message take a count of their own (`__ref(E)`), and a received value is owned (+1).

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
/** A channel: values move through it from one thread to another, in order. */
export interface ChannelCell<T> {
    /** Moves `value` into the channel. */
    send(value: sending T): void;
    /** The next value, which the caller now owns; undefined once the channel is closed and empty. */
    recv(): sending T | undefined;
    /** No more sends. */
    close(): void;
    /** The values until the channel is closed and empty. */
    [Symbol.iterator](): Iterator<T>;
}
/** A channel holding up to `capacity` values; 0 (the default) is unbuffered. */
export declare const Channel: new <T>(capacity?: number) => shared ChannelCell<T>;
/** Starts `f` on a thread of its own. Every argument is sent. */
export declare function go<A extends unknown[]>(f: (...args: A) => void, ...args: sending A): void;
/** Waits until one of the channels has a value or is closed: its index and the value. */
export declare function select<T>(...channels: (shared ChannelCell<T>)[]): [index: number, value: T | undefined];

// @filename: channels.mts
import { SharedArray } from "./own.mjs";
import { Channel, type ChannelCell, go } from "./chan.mjs";

export shared struct Node { value: number; next: Node | null }
export shared struct Box { item: Node | null }

// Sends: the message takes a count of its own, and the sender's count is released as usual.
export function sends(ch: shared ChannelCell<Node>, box: Box, mine: sending Node) {
    const n = new Node();
    ch.send(n);                              // an owned local
    ch.send(new Node());                     // an owned temporary, released after the statement
    ch.send(box.item!);                      // a reference field: retained by `__ref` at once
    ch.send(mine);                           // a parameter declared `sending`
}

// A thread's arguments share a counted value: the same array goes to every thread.
export function threads(ch: shared ChannelCell<Node>, arr: SharedArray<number>) {
    go(worker, arr, ch);
    go(worker, arr, ch);
    return arr.length;
}

// A for-of over a channel owns each value it receives, until the iteration ends.
function worker(arr: SharedArray<number>, ch: shared ChannelCell<Node>) {
    for (const n of ch) arr[0] += n.value;
}

// recv() hands over a count: an owned local, a temporary, or a discarded value released at once.
export function receives(ch: shared ChannelCell<Node>): Node | null {
    const a = ch.recv()!;
    ch.recv();
    const v = a.value + ch.recv()!.value;
    for (const n of ch) {
        if (n.value === v) return n;         // moves the loop variable's count out
    }
    return ch.recv() ?? null;
}

// A stage of a pipeline: what it receives, it sends on (moved).
export function stage(input: shared ChannelCell<SharedArray<number>>, output: shared ChannelCell<SharedArray<number>>) {
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
