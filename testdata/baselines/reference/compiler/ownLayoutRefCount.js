//// [tests/cases/compiler/ownLayoutRefCount.ts] ////

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


//// [list.mts]
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


//// [list.mjs]
import { __F64, __I32, __box, __hold, __newStruct, __quiescent, __ref, __register, __release, __retain, __sharedStruct, __store } from "./own.mjs";
import { log, scope } from "./own.mjs";
export const Node = __sharedStruct("Node", ["value", "next"], [1]);
export const Box = __sharedStruct("Box", ["item"], [0]);
// The linked list: building it (`head` is reassigned, so it is owned, and `node` is new), and
// returning it (the owned local's count moves out).
export function makeList(n) {
    var _a, _b;
    let head = 0;
    try {
        for (let i = 0; i < n; i++) {
            const node = __newStruct(Node);
            try {
                __F64[(node >> 3) + 1] = i;
                __store((node >> 2) + 4, __retain(head)); // a borrow stored in a reference field: retained
                _a = head, head = __retain(node), __release(_a); // the new value is retained before the old is released
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
// Traversal: `n` is reassigned, so it is owned (its declaration moves out of the loop); the loop
// reads a reference field, so each iteration ends at a quiescent point.
export function sum(head) {
    var _a;
    let s = 0;
    {
        let n = __retain(head);
        try {
            for (; n !== 0; _a = n, n = __retain(__I32[(n >> 2) + 4]), __release(_a)) {
                s += __F64[(n >> 3) + 1];
                __quiescent();
            }
        }
        finally {
            __release(n);
        }
    }
    return s;
}
// A function returning a node: a borrow is retained, a new node is returned as it is.
export function second(head) {
    return __retain(__I32[(head >> 2) + 4]);
}
export function created(value) {
    var _a;
    let node = __newStruct(Node);
    try {
        __F64[(node >> 3) + 1] = value;
        return (_a = node, node = 0, _a);
    }
    finally {
        __release(node);
    }
}
export function either(c, p) {
    return c ? __retain(p) : __newStruct(Node); // each branch +1
}
// A field overwritten in a loop: a quiescent point at the end of each iteration (and before
// `continue`). A numeric loop gets none.
export function replace(box, rounds) {
    for (let i = 0; i < rounds; i++) {
        if (i % 2 === 0) {
            __quiescent();
            continue;
        }
        __store((box >> 2) + 2, created(i));
        __quiescent();
    }
    let total = 0;
    for (let i = 0; i < rounds; i++)
        total += i;
    return total;
}
// Rule 1: locals.
export function locals(p, box) {
    var _a, _b;
    const a = p; // a stable borrow: borrowed, no counting
    const b = __newStruct(Node); // owned: released when the block ends
    try {
        const c = __retain(__I32[(box >> 2) + 2]); // an unstable borrow: retained
        try {
            let d = 0; // reassigned: owned
            try {
                _a = d, d = __retain(p), __release(_a);
                _b = d, d = __newStruct(Node), __release(_b);
                let e = 0; // never assigned: 0
                const f = __retain(d); // d may be reassigned: retained
                try {
                    const g = __retain(ambient()); // retained
                    try {
                        const h = __retain(identity(p)); // retained
                        try {
                            return __F64[(a >> 3) + 1] + __F64[(b >> 3) + 1] + (c === 0 ? 0 : __F64[(c >> 3) + 1]) + (e === 0 ? 1 : 0) + __F64[(f >> 3) + 1] + __F64[(g >> 3) + 1] + __F64[(h >> 3) + 1];
                        }
                        finally {
                            __release(h);
                        }
                    }
                    finally {
                        __release(g);
                    }
                }
                finally {
                    __release(f);
                }
            }
            finally {
                __release(d);
            }
        }
        finally {
            __release(c);
        }
    }
    finally {
        __release(b);
    }
}
// Parameters borrow, unless reassigned, defaulted to a value they must own, or of an async function.
export function parameters(n, fallback, optional, none = 0) {
    var _a;
    n = __retain(n);
    fallback = fallback === void 0 ? __newStruct(Node) : __retain(fallback);
    optional ??= 0;
    try {
        _a = n, n = __retain(n || fallback), __release(_a); // `??` on an address is `||` (null is 0)
        switch (none) {
            case 0: return __F64[(n >> 3) + 1] + (optional === 0 ? 0 : __F64[(optional >> 3) + 1]);
        }
        return 0;
    }
    finally {
        __release(fallback);
        __release(n);
    }
}
export async function later(n) {
    n = __retain(n);
    try {
        await null;
        return __F64[(n >> 3) + 1];
    }
    finally {
        __release(n);
    }
}
// Rule 3: an owned value used at once is released after its statement.
export function temporaries(c) {
    var _a, _b, _c, _d, _e;
    __release(created(1));
    const v = (_b = __F64[((_a = created(2)) >> 3) + 1], __release(_a), _b);
    return (_d = 0, _e = v + sum(_c = makeList(3)) + (c ? sum(_d = created(4)) : 0), __release(_c), __release(_d), _e);
}
// Rules 4, 5 and 9: arguments, field stores and null.
function use(n) { return n === 0 ? 0 : __F64[(n >> 3) + 1]; }
export function stores(box, p) {
    var _a, _b, _c;
    use(_a = __retain(__I32[(box >> 2) + 2])); // an unstable borrow passed to a function: retained around the call
    __release(_a);
    use(p); // a stable borrow: as it is
    __store((box >> 2) + 2, __retain(p)); // a borrow: retained
    __store((box >> 2) + 2, created(1)); // owned: stored as it is
    __store((box >> 2) + 2, 0);
    _b = (box >> 2) + 2, __I32[_b] || __store(_b, __retain(p));
    __store((p >> 2) + 4, __retain(__I32[(box >> 2) + 2]));
    __F64[((_c = __retain(__I32[(box >> 2) + 2])) >> 3) + 1] = use(p); // an unstable object and a call in the value: retained first
    __release(_c);
    log(__box(__I32[(box >> 2) + 2])); // boxed: the handle holds its own count
}
// Rules 7 and 8: spawn takes a counted argument, and a closure holds what it captures.
export function threads(box) {
    const list = makeList(3);
    function total() { return sum(list); }
    try {
        __hold(total, list);
        scope(__hold(s => {
            var _a;
            s.spawn(sum, __ref(list));
            s.spawn(stores, __ref(box), __ref(_a = created(4)));
            __release(_a);
        }, list, box));
        return total;
    }
    finally {
        __release(list);
    }
}
__register(import.meta.url, { makeList, sum, second, created, either, replace, locals, parameters, later, temporaries, use, stores, threads });
