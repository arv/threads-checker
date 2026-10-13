//// [tests/cases/compiler/ownLayoutBoundsChecks.ts] ////

//// [own.d.mts]
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

//// [bounds.mts]
import { SharedArray } from "./own.mjs";

export shared struct P { x: number; y: number }

const START = 1;

export function eliminated(a: SharedArray<number>, ps: SharedArray<P>) {
    let sum = 0;
    for (let i = 0; i < a.length; i++) sum += a[i];                      // bound a.length
    const n = a.length;
    for (let i = 0; i < n; ++i) a[i] = sum;                             // bound const n = a.length, ++i
    for (let i = START; i < a.length; i += 1) a[i] += 1;                // init from a const literal, i += 1
    for (let i = 0; i < ps.length; i++) {
        ps[i].x = ps[i].y;                                              // struct elements
        for (let j = 0; j < ps.length; j++) sum += ps[j].x - ps[i].x;   // nested loops
    }
    const local = new SharedArray(Number, 4);
    for (let i = 0; i < local.length; i++) local[i]++;                  // a const array
    return sum;
}

export function kept(a: SharedArray<number>, b: SharedArray<number>, lo: number) {
    let sum = 0;
    for (let i = lo; i < a.length; i++) sum += a[i];                     // init not a literal
    for (let i = -1; i < a.length; i++) sum += a[i];                     // negative init
    for (let i = 0; i < b.length; i++) sum += a[i];                      // bound from another array
    for (let i = 0; i < a.length; i++) { i = 5; sum += a[i]; }           // i assigned in the body
    for (let i = 0; i < a.length; i++) { const f = () => { i++; }; f(); sum += a[i]; } // i written in a closure
    for (let i = 0; i < a.length; i += 2) sum += a[i];                   // update by 2
    let m = a.length;
    m = 100;
    for (let i = 0; i < m; i++) sum += a[i];                             // bound is a reassigned let
    for (let i = 0; i <= a.length; i++) sum += a[i];                     // <= instead of <
    for (var i = 0; i < a.length; i++) sum += a[i];                      // var, not let
    for (let k = 0; k < a.length; k++) [0].forEach(() => { sum += a[k]; }); // inside a closure
    return sum;
}

export function reassigned(a: SharedArray<number>, b: SharedArray<number>) {
    let c = a;
    let sum = 0;
    for (let i = 0; i < c.length; i++) sum += c[i];                      // c is reassigned below
    c = b;
    for (let i = 0; i < a.length; i++) a[i] = sum;                       // a itself is never assigned: eliminated
    return sum;
}


//// [bounds.mjs]
import { __F64, __I32, __OOB, __hold, __newSharedArray, register as __register, __release, __retain, __sharedStruct } from "./own.mjs";
import { SharedArray } from "./own.mjs";
export const P = __sharedStruct("P", ["x", "y"], []);
const START = 1;
export function eliminated(a, ps) {
    let sum = 0;
    for (let i = 0; i < __I32[(a + 4) >> 2]; i++)
        sum += __F64[((a + 16) >> 3) + i]; // bound a.length
    const n = __I32[(a + 4) >> 2];
    for (let i = 0; i < n; ++i)
        __F64[((a + 16) >> 3) + i] = sum; // bound const n = a.length, ++i
    for (let i = START; i < __I32[(a + 4) >> 2]; i += 1)
        __F64[((a + 16) >> 3) + i] += 1; // init from a const literal, i += 1
    for (let i = 0; i < __I32[(ps + 4) >> 2]; i++) {
        __F64[((ps + 16 + i * 24) >> 3) + 1] = __F64[((ps + 16 + i * 24) >> 3) + 2]; // struct elements
        for (let j = 0; j < __I32[(ps + 4) >> 2]; j++)
            sum += __F64[((ps + 16 + j * 24) >> 3) + 1] - __F64[((ps + 16 + i * 24) >> 3) + 1]; // nested loops
    }
    const local = __newSharedArray(8, 4);
    try {
        for (let i = 0; i < __I32[(local + 4) >> 2]; i++)
            __F64[((local + 16) >> 3) + i]++; // a const array
        return sum;
    }
    finally {
        __release(local);
    }
}
export function kept(a, b, lo) {
    let sum = 0;
    for (let i = lo; i < __I32[(a + 4) >> 2]; i++)
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i]; // init not a literal
    for (let i = -1; i < __I32[(a + 4) >> 2]; i++)
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i]; // negative init
    for (let i = 0; i < __I32[(b + 4) >> 2]; i++)
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i]; // bound from another array
    for (let i = 0; i < __I32[(a + 4) >> 2]; i++) {
        i = 5;
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i];
    } // i assigned in the body
    for (let i = 0; i < __I32[(a + 4) >> 2]; i++) {
        const f = () => { i++; };
        f();
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i];
    } // i written in a closure
    for (let i = 0; i < __I32[(a + 4) >> 2]; i += 2)
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i]; // update by 2
    let m = __I32[(a + 4) >> 2];
    m = 100;
    for (let i = 0; i < m; i++)
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i]; // bound is a reassigned let
    for (let i = 0; i <= __I32[(a + 4) >> 2]; i++)
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i]; // <= instead of <
    for (var i = 0; i < __I32[(a + 4) >> 2]; i++)
        sum += (i >>> 0) === i && i < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + i] : __OOB[i]; // var, not let
    for (let k = 0; k < __I32[(a + 4) >> 2]; k++)
        [0].forEach(__hold(() => { sum += (k >>> 0) === k && k < __I32[(a + 4) >> 2] ? __F64[((a + 16) >> 3) + k] : __OOB[k]; }, a)); // inside a closure
    return sum;
}
export function reassigned(a, b) {
    var _a;
    let c = __retain(a);
    try {
        let sum = 0;
        for (let i = 0; i < __I32[(c + 4) >> 2]; i++)
            sum += (i >>> 0) === i && i < __I32[(c + 4) >> 2] ? __F64[((c + 16) >> 3) + i] : __OOB[i]; // c is reassigned below
        _a = c, c = __retain(b), __release(_a);
        for (let i = 0; i < __I32[(a + 4) >> 2]; i++)
            __F64[((a + 16) >> 3) + i] = sum; // a itself is never assigned: eliminated
        return sum;
    }
    finally {
        __release(c);
    }
}
__register(import.meta.url, { eliminated, kept, reassigned });
