//// [tests/cases/compiler/ownLayoutChannelFields.ts] ////

//// [threads.d.mts]
/// <reference lib="esnext.disposable" />
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
declare const int32Brand: unique symbol;
declare const uint8Brand: unique symbol;
declare const layoutBrand: unique symbol;

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

/** A field of a shared struct, declared with `f64`, `i32`, `u8`, `bool`, `str` or `ref<T>()`. */
export interface Field<T> {
  readonly [fieldBrand]: T;
}
/** A number field: an f64 in shared memory. */
export declare const f64: Field<number>;

/**
 * A 32-bit integer: a number, which the field or element it is stored in keeps as an Int32Array would
 * (2 ** 31 as -(2 ** 31)). Atomic operations act on i32s (see `atomic`). The ownership checker
 * requires an integer of a value stored into one (or passed or returned as one): integer arithmetic,
 * which wraps, but not a number that may have a fraction, which must be converted explicitly, as with
 * Math.round(x), Math.trunc(x) or x | 0.
 */
export type i32 = number & { readonly [int32Brand]?: never };
/** An 8-bit unsigned integer: a number, kept as a Uint8Array would (256 as 0, and -1 as 255). As an i32's, its values must be integers. */
export type u8 = number & { readonly [uint8Brand]?: never };
/** The field of an integer, which a SharedArray also takes as its element type. */
export interface IntegerField<T, K extends "i32" | "u8"> extends Field<T> {
  readonly kind: K;
}
/** An i32 field: a 32-bit integer in shared memory, which atomic operations act on. */
export declare const i32: IntegerField<i32, "i32">;
/** A u8 field: a byte in shared memory. */
export declare const u8: IntegerField<u8, "u8">;
/** A boolean field: a byte in shared memory, 0 or 1. */
export declare const bool: Field<boolean>;
/**
 * A string field: the address of the string's one copy in shared memory. Strings are interned, so
 * each distinct string is stored once, and each thread decodes it once. They are never freed.
 */
export declare const str: Field<string>;
/**
 * A reference field: a shared struct, a SharedArray or a Channel, or null. A channel lives as long as
 * the program, so a struct can hand one to any thread: `struct({ changed: ref<Channel<void>>() })`.
 */
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
 * A fixed-length array in shared memory: of numbers (`Number`), of i32s (`i32`), of bytes (`u8`), of
 * booleans (`Boolean`, a byte each), of strings (`String`, interned as `str` fields are), or of shared
 * structs stored inline. Every thread reads and writes the same elements, with no copying; a Barrier,
 * a Channel or an atomic operation orders them.
 */
export interface SharedArray<T, L = ElementLayout<T>> extends Cell {
  readonly length: number;
  [index: number]: T;
  /**
   * Not there at run time: L keeps SharedArrays of numbers, i32s and u8s apart, whose elements
   * TypeScript would otherwise let one stand for another, though they are stored differently. (A
   * property of type ElementLayout<T> would not: TypeScript relates two SharedArrays by T alone.)
   */
  readonly [layoutBrand]?: L;
}
/** How a SharedArray of T stores its elements, as far as telling numbers, i32s and u8s apart goes. */
type ElementLayout<T> = typeof int32Brand extends keyof T ? "i32" : typeof uint8Brand extends keyof T ? "u8" : "other";
export declare const SharedArray: {
  new (type: NumberConstructor, length: number): SharedArray<number>;
  new (type: BooleanConstructor, length: number): SharedArray<boolean>;
  new (type: StringConstructor, length: number): SharedArray<string>;
  new (type: IntegerField<i32, "i32">, length: number): SharedArray<i32>;
  new (type: IntegerField<u8, "u8">, length: number): SharedArray<u8>;
  new <T>(type: StructType<T>, length: number): SharedArray<T & Cell>;
};

// ---- atomic operations ----

/** The fields of a struct type S that are i32s. */
export type I32Fields<S> = { [K in keyof S]-?: typeof int32Brand extends keyof S[K] ? K : never }[keyof S];

/** An atomic operation on an element of a SharedArray<i32>, or on an i32 field of a shared struct. */
export interface AtomicOperation<A extends unknown[], R> {
  (array: SharedArray<i32>, index: number, ...args: A): R;
  <S extends Cell>(struct: S, field: I32Fields<S>, ...args: A): R;
}

/**
 * Atomic operations, which `Atomics` performs: on an element of a SharedArray<i32>, or on an i32 field
 * of a shared struct, named by a string literal. Each is indivisible, and orders the reads and writes
 * around it as a lock would. Compiled, a call is that `Atomics` call, on the i32 itself:
 *
 *     const Work = struct({ next: i32, done: i32 });
 *     for (let i; (i = atomic.add(work, "next", 1)) < n; ) process(i);   // claims item i
 */
export interface Atomic {
  /** The i32's value. */
  readonly load: AtomicOperation<[], i32>;
  /** Stores `value`, and returns it. */
  readonly store: AtomicOperation<[value: i32], i32>;
  /** Adds `value`, and returns the old value. */
  readonly add: AtomicOperation<[value: i32], i32>;
  /** Subtracts `value`, and returns the old value. */
  readonly sub: AtomicOperation<[value: i32], i32>;
  /** Stores the bitwise and of the i32 and `value`, and returns the old value. */
  readonly and: AtomicOperation<[value: i32], i32>;
  /** Stores the bitwise or of the i32 and `value`, and returns the old value. */
  readonly or: AtomicOperation<[value: i32], i32>;
  /** Stores the bitwise xor of the i32 and `value`, and returns the old value. */
  readonly xor: AtomicOperation<[value: i32], i32>;
  /** Stores `value`, and returns the old value. */
  readonly exchange: AtomicOperation<[value: i32], i32>;
  /** Stores `replacement` if the i32 is `expected`, and returns the old value either way. */
  readonly compareExchange: AtomicOperation<[expected: i32, replacement: i32], i32>;
  /**
   * Waits while the i32 is `value`, until notify() wakes the thread or `timeout` ms pass. Not on a
   * page's main thread, which must not block.
   */
  readonly wait: AtomicOperation<[value: i32, timeout?: number], "ok" | "not-equal" | "timed-out">;
  /** Wakes up to `count` threads waiting on the i32 (all of them by default); returns how many it woke. */
  readonly notify: AtomicOperation<[count?: number], number>;
}
export declare const atomic: Atomic;

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

/** A lock: one thread at a time holds it, to change what several threads share. */
export declare class Lock implements Cell {
  readonly [cellBrand]: true;
  constructor();
  /** Blocks until this thread holds the lock; disposing what it returns releases it (`using`). */
  hold(): Disposable;
}

/** A condition: threads holding a lock wait on it until another thread notifies them. */
export declare class Condition implements Cell {
  readonly [cellBrand]: true;
  constructor();
  /** Releases `lock`, which this thread holds, blocks until notified, and holds `lock` again. */
  wait(lock: Lock): void;
  /** Wakes every thread that waits. */
  notifyAll(): void;
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

//// [replica.mts]
import { Channel, i32, ref, select, struct } from "./threads.mjs";

interface Db { open: i32; done: Channel<void> | null }
const Db = struct<Db>({ open: i32, done: ref<Channel<void>>() });
interface Replica { txid: i32; changed: Channel<void> | null; db: Db | null }
const Replica = struct<Replica>({ txid: i32, changed: ref<Channel<void>>(), db: ref<Db>() });

export class DatabaseNotOpenError extends Error {}

// for { notify := r.Notify(); if r.Pos().TXID >= txID { return nil }; select { ... } }
export function waitForPos(r: Replica, txID: i32, done: Channel<void>, cause: () => string): void {
    for (;;) {
        const notify = r.changed ??= new Channel<void>();
        if (r.txid >= txID) return;
        const [which] = select(notify, done, r.db!.done!);
        if (which === 1) throw new Error(`wait for replica position ${txID} (at ${r.txid})`, { cause: cause() });
        if (which === 2) throw new DatabaseNotOpenError();
    }
}

// a change: the new position, and every waiter woken by closing the channel it waits on
export function advance(r: Replica, txid: i32) {
    r.txid = txid;
    const changed = r.changed;
    r.changed = new Channel<void>();
    changed?.close();
}

export function open(): Replica {
    const db = new Db(), r = new Replica();
    db.done = new Channel<void>();
    const same = (r.db = db);
    return same === db ? r : r;
}

//// [barrier.mts]
// A Lock and a Condition last as long as the program, as a channel does: a struct holds them the same
// way. A barrier for n threads:
import { Condition, i32, Lock, ref, struct } from "./threads.mjs";

interface Barrier { n: i32; waiting: i32; round: i32; lock: Lock | null; changed: Condition | null }
const Barrier = struct<Barrier>({ n: i32, waiting: i32, round: i32, lock: ref<Lock>(), changed: ref<Condition>() });

export function barrier(n: i32): Barrier {
    const b = new Barrier();
    b.n = n;
    b.lock = new Lock();
    b.changed = new Condition();
    return b;
}

// returns once all n threads have called it
export function wait(b: Barrier) {
    const lock = b.lock!, changed = b.changed!;
    using _ = lock.hold();
    const round = b.round;
    if (++b.waiting === b.n) {
        b.waiting = 0;
        b.round = round + 1;
        changed.notifyAll();
    } else while (b.round === round) changed.wait(lock);
}

//// [syntax.mts]
// The syntax's shared structs take channel fields as well.
import { Channel } from "./threads.mjs";
shared struct Mailbox { count: number; inbox: Channel<string> | null }
export function post(m: Mailbox, text: string) {
    (m.inbox ??= new Channel<string>(8)).send(text);
    m.count++;
}


//// [replica.mjs]
import { I32 as __I32, box as __box, cellAddress as __cellAddress, newStruct as __newStruct, quiescent as __quiescent, register as __register, release as __release, retain as __retain, sharedStruct as __sharedStruct, store as __store } from "./threads.mjs";
import { Channel, i32, ref, select, struct } from "./threads.mjs";
const Db = __sharedStruct("Db", ["open", "done"], [], "ic");
const Replica = __sharedStruct("Replica", ["txid", "changed", "db"], [2], "icr");
export class DatabaseNotOpenError extends Error {
}
// for { notify := r.Notify(); if r.Pos().TXID >= txID { return nil }; select { ... } }
export function waitForPos(r, txID, done, cause) {
    var _a, _b;
    for (;;) {
        const notify = (_a = (r >> 2) + 4, __box(__I32[_a]) ?? ((__I32[_a] = __cellAddress(_b = new Channel()), _b)));
        if (__I32[(r >> 2) + 2] >= txID)
            return;
        const [which] = select(notify, done, __box(__I32[(__I32[(r >> 2) + 6] >> 2) + 4]));
        if (which === 1)
            throw new Error(`wait for replica position ${txID} (at ${__I32[(r >> 2) + 2]})`, { cause: cause() });
        if (which === 2)
            throw new DatabaseNotOpenError();
        __quiescent();
    }
}
// a change: the new position, and every waiter woken by closing the channel it waits on
export function advance(r, txid) {
    __I32[(r >> 2) + 2] = txid;
    const changed = __box(__I32[(r >> 2) + 4]);
    __I32[(r >> 2) + 4] = __cellAddress(new Channel());
    changed?.close();
}
export function open() {
    var _a, _b;
    let db = __newStruct(Db), r = __newStruct(Replica);
    try {
        __I32[(db >> 2) + 4] = __cellAddress(new Channel());
        const same = (__retain(__store((r >> 2) + 6, __retain(db))));
        try {
            return same === db ? (_a = r, r = 0, _a) : (_b = r, r = 0, _b);
        }
        finally {
            __release(same);
        }
    }
    finally {
        __release(r);
        __release(db);
    }
}
__register(import.meta.url, { waitForPos, advance, open });
//// [barrier.mjs]
var __addDisposableResource = (this && this.__addDisposableResource) || function (env, value, async) {
    if (value !== null && value !== void 0) {
        if (typeof value !== "object" && typeof value !== "function") throw new TypeError("Object expected.");
        var dispose, inner;
        if (async) {
            if (!Symbol.asyncDispose) throw new TypeError("Symbol.asyncDispose is not defined.");
            dispose = value[Symbol.asyncDispose];
        }
        if (dispose === void 0) {
            if (!Symbol.dispose) throw new TypeError("Symbol.dispose is not defined.");
            dispose = value[Symbol.dispose];
            if (async) inner = dispose;
        }
        if (typeof dispose !== "function") throw new TypeError("Object not disposable.");
        if (inner) dispose = function() { try { inner.call(this); } catch (e) { return Promise.reject(e); } };
        env.stack.push({ value: value, dispose: dispose, async: async });
    }
    else if (async) {
        env.stack.push({ async: true });
    }
    return value;
};
var __disposeResources = (this && this.__disposeResources) || (function (SuppressedError) {
    return function (env) {
        function fail(e) {
            env.error = env.hasError ? new SuppressedError(e, env.error, "An error was suppressed during disposal.") : e;
            env.hasError = true;
        }
        var r, s = 0;
        function next() {
            while (r = env.stack.pop()) {
                try {
                    if (!r.async && s === 1) return s = 0, env.stack.push(r), Promise.resolve().then(next);
                    if (r.dispose) {
                        var result = r.dispose.call(r.value);
                        if (r.async) return s |= 2, Promise.resolve(result).then(next, function(e) { fail(e); return next(); });
                    }
                    else s |= 1;
                }
                catch (e) {
                    fail(e);
                }
            }
            if (s === 1) return env.hasError ? Promise.reject(env.error) : Promise.resolve();
            if (env.hasError) throw env.error;
        }
        return next();
    };
})(typeof SuppressedError === "function" ? SuppressedError : function (error, suppressed, message) {
    var e = new Error(message);
    return e.name = "SuppressedError", e.error = error, e.suppressed = suppressed, e;
});
import { I32 as __I32, box as __box, cellAddress as __cellAddress, newStruct as __newStruct, register as __register, release as __release, sharedStruct as __sharedStruct } from "./threads.mjs";
// A Lock and a Condition last as long as the program, as a channel does: a struct holds them the same
// way. A barrier for n threads:
import { Condition, i32, Lock, ref, struct } from "./threads.mjs";
const Barrier = __sharedStruct("Barrier", ["n", "waiting", "round", "lock", "changed"], [], "iiicc");
export function barrier(n) {
    var _a;
    let b = __newStruct(Barrier);
    try {
        __I32[(b >> 2) + 2] = n;
        __I32[(b >> 2) + 8] = __cellAddress(new Lock());
        __I32[(b >> 2) + 10] = __cellAddress(new Condition());
        return (_a = b, b = 0, _a);
    }
    finally {
        __release(b);
    }
}
// returns once all n threads have called it
export function wait(b) {
    const env_1 = { stack: [], error: void 0, hasError: false };
    try {
        const lock = __box(__I32[(b >> 2) + 8]), changed = __box(__I32[(b >> 2) + 10]);
        const _ = __addDisposableResource(env_1, lock.hold(), false);
        const round = __I32[(b >> 2) + 6];
        if (++__I32[(b >> 2) + 4] === __I32[(b >> 2) + 2]) {
            __I32[(b >> 2) + 4] = 0;
            __I32[(b >> 2) + 6] = round + 1;
            changed.notifyAll();
        }
        else
            while (__I32[(b >> 2) + 6] === round)
                changed.wait(lock);
    }
    catch (e_1) {
        env_1.error = e_1;
        env_1.hasError = true;
    }
    finally {
        __disposeResources(env_1);
    }
}
__register(import.meta.url, { barrier, wait });
//// [syntax.mjs]
import { F64 as __F64, I32 as __I32, box as __box, cellAddress as __cellAddress, register as __register, sharedStruct as __sharedStruct } from "./threads.mjs";
// The syntax's shared structs take channel fields as well.
import { Channel } from "./threads.mjs";
const Mailbox = __sharedStruct("Mailbox", ["count", "inbox"], [], "fc");
export function post(m, text) {
    var _a, _b;
    (_a = (m >> 2) + 4, __box(__I32[_a]) ?? ((__I32[_a] = __cellAddress(_b = new Channel(8)), _b))).send(text);
    __F64[(m >> 3) + 1]++;
}
__register(import.meta.url, { post });
