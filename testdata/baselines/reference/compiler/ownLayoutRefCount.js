//// [tests/cases/compiler/ownLayoutRefCount.ts] ////

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

/** A thread; joining it moves the thread's result back. */
export interface Thread<R> extends Cell {
  join(): Sending<R>;
}
/** Spawns threads that cannot outlive the scope. Every argument is sent to the new thread. */
export interface Scope extends Cell {
  spawn<A extends unknown[], R>(f: (...args: A) => Sending<R>, ...args: Sending<A>): Thread<R>;
}
/** Runs `body` and joins every thread it spawned before returning. */
export declare function scope<R>(body: (s: Scope) => R): R;

/** Prints a line, synchronously, from any thread. */
export declare function log(...args: unknown[]): void;

//// [list.mts]
import { f64, log, ref, scope, struct, type Struct } from "./threads.mjs";

export interface Node { value: number; next: Node | null }
export const Node = struct<Node>({ value: f64, next: ref<Node>() });
export const Box = struct({ item: ref<Node>() });
export type Box = Struct<typeof Box>;

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
import { F64 as __F64, I32 as __I32, box as __box, capture as __capture, countedRef as __countedRef, newStruct as __newStruct, quiescent as __quiescent, register as __register, release as __release, retain as __retain, sharedStruct as __sharedStruct, store as __store } from "./threads.mjs";
import { f64, log, ref, scope, struct } from "./threads.mjs";
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
        __capture(total, list);
        scope(__capture(s => {
            var _a;
            s.spawn(sum, __countedRef(list));
            s.spawn(stores, __countedRef(box), __countedRef(_a = created(4)));
            __release(_a);
        }, list, box));
        return total;
    }
    finally {
        __release(list);
    }
}
__register(import.meta.url, { makeList, sum, second, created, either, replace, locals, parameters, later, temporaries, use, stores, threads });
