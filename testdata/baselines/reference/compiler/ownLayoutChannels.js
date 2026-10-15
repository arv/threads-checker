//// [tests/cases/compiler/ownLayoutChannels.ts] ////

//// [threads.d.mts]
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

//// [channels.mts]
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


//// [channels.mjs]
import { F64 as __F64, I32 as __I32, countedRef as __countedRef, newSharedArray as __newSharedArray, newStruct as __newStruct, outOfBounds as __outOfBounds, register as __register, release as __release, sharedStruct as __sharedStruct } from "./threads.mjs";
import { Channel, f64, go, ref, SharedArray, struct } from "./threads.mjs";
export const Node = __sharedStruct("Node", ["value", "next"], [1]);
export const Box = __sharedStruct("Box", ["item"], [0]);
// Sends: the message takes a count of its own, and the sender's count is released as usual.
export function sends(ch, box, mine) {
    var _a;
    const n = __newStruct(Node);
    try {
        ch.send(__countedRef(n)); // an owned local
        ch.send(__countedRef(_a = __newStruct(Node))); // an owned temporary, released after the statement
        __release(_a);
        ch.send(__countedRef(__I32[(box >> 2) + 2])); // a reference field: retained by `__ref` at once
        ch.send(__countedRef(mine)); // a parameter declared `Sending<Node>`
    }
    finally {
        __release(n);
    }
}
// A thread's arguments share a counted value: the same array goes to every thread.
export function threads(ch, arr) {
    go(worker, __countedRef(arr), ch);
    go(worker, __countedRef(arr), ch);
    return __I32[(arr + 4) >> 2];
}
// A for-of over a channel owns each value it receives, until the iteration ends.
function worker(arr, ch) {
    for (const n of ch) {
        try {
            __F64[((arr + 16) >> 3) + ((0 >>> 0) === 0 && 0 < __I32[(arr + 4) >> 2] ? 0 : __outOfBounds[0])] += __F64[(n >> 3) + 1];
        }
        finally {
            __release(n);
        }
    }
}
// recv() hands over a count: an owned local, a temporary, or a discarded value released at once.
export function receives(ch) {
    var _a, _b, _c;
    const a = ch.recv();
    try {
        __release(ch.recv());
        const v = (_b = __F64[(a >> 3) + 1] + __F64[((_a = ch.recv()) >> 3) + 1], __release(_a), _b);
        for (let n of ch) {
            try {
                if (__F64[(n >> 3) + 1] === v)
                    return (_c = n, n = 0, _c); // moves the loop variable's count out
            }
            finally {
                __release(n);
            }
        }
        return ch.recv() || 0;
    }
    finally {
        __release(a);
    }
}
// A stage of a pipeline: what it receives, it sends on (moved).
export function stage(input, output) {
    for (const block of input) {
        try {
            __F64[((block + 16) >> 3) + ((0 >>> 0) === 0 && 0 < __I32[(block + 4) >> 2] ? 0 : __outOfBounds[0])]++;
            output.send(__countedRef(block));
        }
        finally {
            __release(block);
        }
    }
    output.close();
}
export function pipeline(blocks) {
    const input = new Channel(4), output = new Channel(4);
    go(stage, input, output);
    for (let k = 0; k < blocks; k++) {
        const block = __newSharedArray(8, 16);
        try {
            input.send(__countedRef(block));
        }
        finally {
            __release(block);
        }
    }
    input.close();
    let total = 0;
    for (const block of output) {
        try {
            total += (0 >>> 0) === 0 && 0 < __I32[(block + 4) >> 2] ? __F64[((block + 16) >> 3) + 0] : __outOfBounds[0];
        }
        finally {
            __release(block);
        }
    }
    return total;
}
__register(import.meta.url, { sends, threads, worker, receives, stage, pipeline });
