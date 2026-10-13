//// [tests/cases/compiler/ownMarkers.ts] ////

//// [threads.d.mts]
// Threads for JavaScript in plain TypeScript: own and shared values, shared structs, and channels.
//
// Code written against these types is ordinary TypeScript: tsc, editors, linters and formatters
// accept it as it is. Two tools add what the types cannot say:
//   - the ownership checker (`tsgo own check`, from the tsgo fork) runs in CI like a linter and
//     proves the rules below: nothing is used after it is sent, and shared values never change;
//   - the bundler plugin (vite-plugin-threads.mjs) compiles shared structs and SharedArrays to
//     typed-array accesses on shared memory, counts their references, and registers the functions
//     threads run.
//
// Every value a thread touches is one of three kinds:
//   - owned, the default: an ordinary object only one thread can reach. `Sending<T>` moves it: the
//     caller gives it up and the receiver owns it;
//   - shared: `Shared<T>`, deeply immutable, so any number of threads may read it;
//   - a cell: shared and mutable, but only through what synchronizes it (Barrier, Slots, Channel), or,
//     for shared structs and SharedArrays, in place, ordered by one of those.

declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;

/** A cell: shared and mutable. Any number of threads may hold the same one. */
export interface Cell {
  readonly [cellBrand]: true;
}

/**
 * A value whose ownership moves, on a parameter or a result: the caller must not use it, or anything
 * it reaches, afterwards. The ownership checker enforces this; to TypeScript it is just T.
 */
export type Sending<T> = T;

/** A deeply immutable value, which any number of threads may read. To TypeScript it is deeply readonly. */
export type Shared<T> = T extends Cell | ((...args: any[]) => any) ? T
  : T extends object ? { readonly [K in keyof T]: Shared<T[K]> }
  : T;

/** Ends `x`'s lifetime: its value, and what it reaches, moves to the caller. At run time, `x` itself. */
export declare function consume<T>(x: T): Sending<T>;

/** Freezes a value the caller owns into a deeply immutable value every thread can read. */
export declare function share<T>(value: Sending<T>): Shared<T>;

// ---- shared structs ----

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

/**
 * Declares a shared struct: fields at fixed places in shared memory, which every thread reads and
 * writes in place. Reads and writes are not ordered between threads: a Barrier or a Channel orders
 * them.
 *
 *     const Body = struct({ x: f64, y: f64, m: f64 });
 *     type Body = Struct<typeof Body>;
 *
 * A struct that refers to its own type is declared with an interface:
 *
 *     interface Node { value: number; next: Node | null }
 *     const Node = struct<Node>({ value: f64, next: ref<Node>() });
 */
export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;
export declare function struct<T>(fields: { [K in keyof T]-?: Field<T[K]> }): StructType<T>;

/**
 * A fixed-length array in shared memory, of numbers or of shared structs stored inline. Every thread
 * reads and writes the same elements, with no copying; a Barrier or a Channel orders them.
 */
export interface SharedArray<T> extends Cell {
  readonly length: number;
  [index: number]: T;
}
export declare const SharedArray: {
  new (type: NumberConstructor, length: number): SharedArray<number>;
  new <T>(type: StructType<T>, length: number): SharedArray<T & Cell>;
};

// ---- threads ----

/** A thread; joining it moves the thread's result back. */
export interface Thread<R> extends Cell {
  /** Waits for the thread, from a thread that may block (not a page's main thread). */
  join(): Sending<R>;
  /** Waits for the thread without blocking: for a page's main thread. */
  joinAsync(): Promise<Sending<R>>;
}

/** Starts `f` on a thread. Every argument is sent: owned values move, shared values and cells are shared. */
export declare function spawn<A extends unknown[], R>(f: (...args: A) => Sending<R>, ...args: Sending<A>): Thread<R>;

/** Spawns threads that cannot outlive the scope. */
export interface Scope extends Cell {
  spawn<A extends unknown[], R>(f: (...args: A) => Sending<R>, ...args: Sending<A>): Thread<R>;
}
/** Runs `body` and joins every thread it spawned before returning. */
export declare function scope<R>(body: (s: Scope) => R): R;

/** Splits an owned array into `parts` owned, disjoint chunks of nearly equal size. Trusted. */
export declare function partition<T>(items: Sending<T[]>, parts: number): Sending<T[]>[];
/** Takes element `index` out of an array of owned values, so the caller owns it. Trusted. */
export declare function take<T>(items: Sending<T>[], index: number): Sending<T>;

/** A reusable barrier for a fixed number of threads. */
export declare class Barrier implements Cell {
  readonly [cellBrand]: true;
  constructor(threads: number);
  /** Blocks until all threads have called `wait`. */
  wait(): void;
}

/** One slot per thread: a thread publishes a shared value into its slot, and everyone reads every slot. */
export declare class Slots<T> implements Cell {
  readonly [cellBrand]: true;
  constructor(count: number);
  readonly length: number;
  set(index: number, value: Shared<T>): void;
  get(index: number): Shared<T> | undefined;
  /** The arrays in all slots, concatenated in slot order. */
  flat(): Shared<T extends readonly (infer E)[] ? E[] : T[]>;
}

// ---- goroutines and channels ----

/** A channel: values move through it from thread to thread, in order. */
export declare class Channel<T> implements Cell {
  readonly [cellBrand]: true;
  /** Holds up to `capacity` values; 0, the default, is unbuffered. */
  constructor(capacity?: number);
  /** Moves `value` in: waits while the channel is full, or, unbuffered, until a receiver takes it. */
  send(value: Sending<T>): void;
  /** The next value, which the caller now owns; waits while empty. Undefined once closed and empty. */
  recv(): Sending<T> | undefined;
  /** No more sends. Receivers still get what is left. */
  close(): void;
  [Symbol.iterator](): Iterator<T>;
}
/** Starts `f` on a thread of its own, a goroutine. Nothing waits for it. */
export declare function go<A extends unknown[]>(f: (...args: A) => void, ...args: Sending<A>): void;
/** Waits until one of the channels has a value or is closed: its index and the value (undefined if closed). */
export declare function select<T>(...channels: Channel<T>[]): [index: number, value: T | undefined];

/** Prints a line, synchronously, from any thread. */
export declare function log(...args: unknown[]): void;
/** Shared memory in use, and shared structs and SharedArrays allocated and freed. */
export declare function heapStats(): { heapBytes: number; allocated: number; freed: number; pending: number };

//// [markers.mts]
import { Barrier, Channel, consume, f64, go, ref, scope, share, type Sending, type Shared, SharedArray, spawn, struct, type Struct } from "./threads.mjs";

// A struct with an object type: `Struct<typeof Body>` is its values' type.
const Body = struct({ x: f64, y: f64, m: f64 });
type Body = Struct<typeof Body>;

// A struct that refers to itself, with an interface; and one that refers to it.
interface Node { value: number; next: Node | null }
const Node = struct<Node>({ value: f64, next: ref<Node>() });
const Box = struct({ item: ref<Node>(), bodies: ref<SharedArray<Body>>() });
type Box = Struct<typeof Box>;

// Typed layouts and reference counting, as with `shared struct`.
export function makeList(n: number): Node | null {
    let head: Node | null = null;
    for (let i = 0; i < n; i++) {
        const node = new Node();
        node.value = i;
        node.next = head;
        head = node;
    }
    return head;
}

export function energy(bodies: SharedArray<Body>): number {
    let e = 0;
    for (let i = 0; i < bodies.length; i++) {
        const b = bodies[i];
        e += 0.5 * b.m * (b.x * b.x + b.y * b.y);
    }
    return e;
}

// Cells (Barrier, Channel, Thread, a struct, a SharedArray) are shared: any number of threads get
// the same one.
function step(lo: number, bodies: SharedArray<Body>, barrier: Barrier, box: Box) {
    barrier.wait();
}
export function simulate(threads: number) {
    const bodies = new SharedArray(Body, 100), barrier = new Barrier(threads), box = new Box();
    scope(s => {
        for (let t = 0; t < threads; t++) s.spawn(step, t, bodies, barrier, box);
    });
    return energy(bodies);
}

// Sending<T> moves an owned value, Shared<T> is shared, and consume(x) is x.
function total(xs: Sending<number[]>): number { return xs.reduce((a, b) => a + b, 0); }
function read(table: Shared<number[]>): number { return table[0]; }
export function owned() {
    const xs = [1, 2, 3];
    const thread = spawn(total, xs);
    const table = share([4, 5, 6]);
    spawn(read, table);
    spawn(read, table);
    const ys = [7];
    const zs = consume(ys);
    return [thread, zs];
}

// Channels of counted values.
function produce(out: Channel<SharedArray<number>>) {
    for (let k = 0; k < 4; k++) out.send(new SharedArray(Number, 16));
    out.close();
}
export function consumeAll() {
    const ch = new Channel<SharedArray<number>>(2);
    go(produce, ch);
    let sum = 0;
    for (const block of ch) sum += block.length;
    return sum;
}


//// [markers.mjs]
import { F64 as __F64, I32 as __I32, capture as __capture, countedRef as __countedRef, newSharedArray as __newSharedArray, newStruct as __newStruct, quiescent as __quiescent, register as __register, release as __release, retain as __retain, sharedStruct as __sharedStruct, store as __store } from "./threads.mjs";
import { Barrier, Channel, consume, f64, go, ref, scope, share, SharedArray, spawn, struct } from "./threads.mjs";
// A struct with an object type: `Struct<typeof Body>` is its values' type.
const Body = __sharedStruct("Body", ["x", "y", "m"], []);
const Node = __sharedStruct("Node", ["value", "next"], [1]);
const Box = __sharedStruct("Box", ["item", "bodies"], [0, 1]);
// Typed layouts and reference counting, as with `shared struct`.
export function makeList(n) {
    var _a, _b;
    let head = 0;
    try {
        for (let i = 0; i < n; i++) {
            const node = __newStruct(Node);
            try {
                __F64[(node >> 3) + 1] = i;
                __store((node >> 2) + 4, __retain(head));
                _a = head, head = __retain(node), __release(_a);
            }
            finally {
                __release(node);
            }
            __quiescent();
        }
        return (_b = head, head = 0, _b);
    }
    finally {
        __release(head);
    }
}
export function energy(bodies) {
    let e = 0;
    for (let i = 0; i < __I32[(bodies + 4) >> 2]; i++) {
        const b = bodies + 16 + i * 32;
        e += 0.5 * __F64[(b >> 3) + 3] * (__F64[(b >> 3) + 1] * __F64[(b >> 3) + 1] + __F64[(b >> 3) + 2] * __F64[(b >> 3) + 2]);
    }
    return e;
}
// Cells (Barrier, Channel, Thread, a struct, a SharedArray) are shared: any number of threads get
// the same one.
function step(lo, bodies, barrier, box) {
    barrier.wait();
}
export function simulate(threads) {
    const bodies = __newSharedArray(Body, 100), barrier = new Barrier(threads), box = __newStruct(Box);
    try {
        scope(__capture(s => {
            for (let t = 0; t < threads; t++)
                s.spawn(step, t, __countedRef(bodies), barrier, __countedRef(box));
        }, bodies, box));
        return energy(bodies);
    }
    finally {
        __release(box);
        __release(bodies);
    }
}
// Sending<T> moves an owned value, Shared<T> is shared, and consume(x) is x.
function total(xs) { return xs.reduce((a, b) => a + b, 0); }
function read(table) { return table[0]; }
export function owned() {
    const xs = [1, 2, 3];
    const thread = spawn(total, xs);
    const table = share([4, 5, 6]);
    spawn(read, table);
    spawn(read, table);
    const ys = [7];
    const zs = ys;
    return [thread, zs];
}
// Channels of counted values.
function produce(out) {
    var _a;
    for (let k = 0; k < 4; k++) {
        out.send(__countedRef(_a = __newSharedArray(8, 16)));
        __release(_a);
    }
    out.close();
}
export function consumeAll() {
    const ch = new Channel(2);
    go(produce, ch);
    let sum = 0;
    for (const block of ch) {
        try {
            sum += __I32[(block + 4) >> 2];
        }
        finally {
            __release(block);
        }
    }
    return sum;
}
__register(import.meta.url, { makeList, energy, step, simulate, total, read, owned, produce, consumeAll });
