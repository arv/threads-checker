// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./own.mjs

// Reference counting of shared structs and SharedArrays (REFCOUNT.md in porffor's
// threads/escape-proto): every address is owned (+1), a stable borrow or an unstable borrow, and the
// emit retains and releases where the rules say.

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


// @filename: list.mts
import { log, scope } from "./own.mjs";

export shared struct Node { value: number; next: Node | null }
export shared struct Box { item: Node | null }

// The linked list: building it (`head` is reassigned, so it is owned, and `node` is new), and
// returning it (the owned local's count moves out).
export function makeList(n: number): Node | null {
    let head: Node | null = null;
    for (let i = 0; i < n; i++) {
        const node = new Node();
        node.value = i;
        node.next = head;                    // a borrow stored in a reference field: retained
        head = node;                         // the new value is retained before the old is released
    }
    return head;
}

// Traversal: `n` is reassigned, so it is owned (its declaration moves out of the loop); the loop
// reads a reference field, so each iteration ends at a quiescent point.
export function sum(head: Node | null): number {
    let s = 0;
    for (let n = head; n !== null; n = n.next) s += n.value;
    return s;
}

// A function returning a node: a borrow is retained, a new node is returned as it is.
export function second(head: Node): Node | null {
    return head.next;
}
export function created(value: number): Node {
    const node = new Node();
    node.value = value;
    return node;
}
export function either(c: boolean, p: Node): Node {
    return c ? p : new Node();               // each branch +1
}

// A field overwritten in a loop: a quiescent point at the end of each iteration (and before
// `continue`). A numeric loop gets none.
export function replace(box: Box, rounds: number) {
    for (let i = 0; i < rounds; i++) {
        if (i % 2 === 0) continue;
        box.item = created(i);
    }
    let total = 0;
    for (let i = 0; i < rounds; i++) total += i;
    return total;
}

declare function ambient(): Node;            // not compiled here: its result is a borrow
declare function identity<T>(x: T): T;       // generic: its result is a borrow

// Rule 1: locals.
export function locals(p: Node, box: Box) {
    const a = p;                             // a stable borrow: borrowed, no counting
    const b = new Node();                    // owned: released when the block ends
    const c = box.item;                      // an unstable borrow: retained
    let d: Node | null = null;               // reassigned: owned
    d = p;
    d = new Node();
    let e: Node | undefined;                 // never assigned: 0
    const f = d;                             // d may be reassigned: retained
    const g = ambient();                     // retained
    const h = identity(p);                   // retained
    return a.value + b.value + (c === null ? 0 : c.value) + (e === undefined ? 1 : 0) + f.value + g.value + h.value;
}

// Parameters borrow, unless reassigned, defaulted to a value they must own, or of an async function.
export function parameters(n: Node | null, fallback: Node = new Node(), optional?: Node, none: Node | null = null) {
    n = n ?? fallback;                       // `??` on an address is `||` (null is 0)
    switch (none) {
        case null: return n.value + (optional === undefined ? 0 : optional.value);
    }
    return 0;
}
export async function later(n: Node) {
    await null;
    return n.value;
}

// Rule 3: an owned value used at once is released after its statement.
export function temporaries(c: boolean): number {
    created(1);
    const v = created(2).value;
    return v + sum(makeList(3)) + (c ? sum(created(4)) : 0);
}

// Rules 4, 5 and 9: arguments, field stores and null.
function use(n: Node | null): number { return n === null ? 0 : n.value; }
export function stores(box: Box, p: Node) {
    use(box.item);                           // an unstable borrow passed to a function: retained around the call
    use(p);                                  // a stable borrow: as it is
    box.item = p;                            // a borrow: retained
    box.item = created(1);                   // owned: stored as it is
    box.item = null;
    box.item ??= p;
    p.next = box.item;
    box.item!.value = use(p);                // an unstable object and a call in the value: retained first
    log(box.item);                           // boxed: the handle holds its own count
}

// Rules 7 and 8: spawn takes a counted argument, and a closure holds what it captures.
export function threads(box: Box) {
    const list = makeList(3);
    scope(s => {
        s.spawn(sum, list);
        s.spawn(stores, box, created(4));
    });
    function total() { return sum(list); }
    return total;
}
