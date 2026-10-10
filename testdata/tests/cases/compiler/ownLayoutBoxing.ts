// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./own.mjs

// Boxing: a shared struct or SharedArray value is boxed (`__box`) where it flows into a place typed
// otherwise, and unboxed (`__unbox`) where such a value flows into a struct-typed place.

// @filename: own.d.mts
// Types for the own/shared threads runtime own.mjs (the runtime itself lives in porffor). Programs
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

// @filename: boxing.mts
import { log, SharedArray } from "./own.mjs";

export shared struct Vec { x: number; y: number }

declare function takeVec(v: Vec): void;
declare function takeUnknown(u: unknown): void;
declare function identity<T>(x: T): T;
declare const anything: any;

export function roundTrip(v: Vec) {
    const u: unknown = v;                    // box
    const v2 = u as Vec;                     // unbox
    return v2.x;
}

export function boxed(v: Vec, other: unknown, a: SharedArray<number>) {
    log(v, a);                               // host function arguments
    takeUnknown(v);
    const text = `${v} and ` + v;            // strings
    const kind = typeof v;
    const same = v === other;                // equality with a non-struct value
    const isNull = v === null;               // stays raw
    const union: Vec | number = v;
    const object: object = v;
    const list: unknown[] = [v, a];
    const record: { v: unknown } = { v };
    const copy = { ...v };
    const s = v.toString();                  // not a field
    const { x } = v;                         // destructured through the handle
    const has = "x" in v;
    const same2 = identity(v);               // generic: decided by the instantiation, stays raw
    return [text, kind, same, isNull, union, object, list, record, copy, s, x, has, same2.y];
}

export function unboxed(u: unknown, a: any) {
    takeVec(anything);                       // any into a struct parameter
    const v = u as Vec;
    const w: Vec = a;
    if (isVec(u)) {
        u.x = 1;                             // narrowed: unboxed where the layout is used
        takeVec(u);
    }
    return v.y + w.y;
}

function isVec(u: unknown): u is Vec {
    return u !== null && typeof u === "object" && "x" in u;
}

export function returns(v: Vec): unknown {
    return v;                                // box
}

export function returnsVec(u: unknown): Vec {
    return u as Vec;                         // unbox (the assertion)
}

// @filename: assertion.ts
import type { Vec } from "./boxing.mjs";
export function angleBracket(u: unknown) {
    return (<Vec>u).x;                       // unbox (angle-bracket assertion)
}
