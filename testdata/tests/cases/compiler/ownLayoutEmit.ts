// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./own.mjs

// Typed layouts of shared structs and SharedArrays with `ownRuntime`: field and element accesses
// become typed-array accesses on the shared heap.

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

// @filename: layout.mts
import { SharedArray } from "./own.mjs";

export shared struct Vec { x: number; y: number; z: number }
export shared struct Particle { pos: Vec; mass: number; trail: SharedArray<number> }

declare function make(): Vec;
declare function pick(): number;
declare function arrays(): SharedArray<Vec>;

export function fields(v: Vec, p: Particle) {
    const x = v.x;                          // read: slot 1
    v.z = x + v.y;                          // write
    v.x += 1;                               // compound assignment
    v.y++;                                  // postfix
    --v.z;                                  // prefix
    const m = make().y;                     // object with side effects: no temporary needed
    p.pos.x = p.mass;                       // chain through a struct-typed field
    return v === p.pos;                     // addresses compare as numbers
}

export function elements(vs: SharedArray<Vec>, i: number) {
    const v = vs[i];                        // element address, bounds-checked
    vs[i].x = vs[i + 1].y;                  // chains; index not simple: temporary
    const n = vs.length;                    // header word
    const w = arrays()[pick()];             // array and index not simple: temporaries
    return n + w.z + v.x;
}

export function numbers(a: SharedArray<number>, p: Particle, i: number) {
    const first = a[0];                     // read
    a[i] = first * 2;                       // write
    a[i + 1] += 3;                          // compound assignment, index not simple
    a[i]++;
    p.trail[i] = a.length;                  // array from a field: temporary
    return a[i];
}

export function allocate(n: number) {
    const v = new Vec();
    const vs = new SharedArray(Vec, n);
    const ns = new SharedArray(Number, n);
    ns[0] = v.x;
    return vs;
}
