// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./own.mjs
// @noEmit: true

// Errors of reference counting: what the emit cannot count.

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


// @filename: errors.mts
export shared struct Node { value: number; next: Node | null }

let current: Node | null = null;             // a module's variable, which functions capture
export function setCurrent(n: Node) {
    current = n;                             // error: captured and reassigned
}

export function captured(head: Node) {
    let n = head;
    const f = () => n.value;
    n = new Node();                          // error: captured and reassigned
    return f;
}

export function containers(n: Node, list: Node[]) {
    const array = [n];                       // error: an address in an ordinary array
    const object = { n };                    // error: in an ordinary object
    const named = { node: n };               // error
    list[0] = n;                             // error
    const holder: { node: Node | null } = { node: null };
    holder.node = n;                         // error
    const boxed: unknown[] = [n];            // a handle: fine
    return [array, object, named, boxed];
}

export class Holder {
    node: Node = new Node();                 // error
    constructor(public other: Node) {}       // error
}

export function destructuring(pair: { a: Node; b: number }, list: Node[]) {
    const { a, b } = pair;                   // error: a
    let x: Node | null = null;
    [x] = list;                              // error
    for (const n of list) {}                 // error: a for-of variable
    return [a, b, x];
}

export function members(n: Node) {
    const o = { get value() { return n.value; } };   // error: a method captures n
    class C { v = n.value; }                          // error: a class captures n
    return [o, C];
}

export function* generator(n: Node) {       // error: a generator runs after its caller's borrow ends
    yield n.value;
}

export function rest(...nodes: Node[]) {    // error: the arguments are stored in an ordinary array
    return nodes.length;
}
