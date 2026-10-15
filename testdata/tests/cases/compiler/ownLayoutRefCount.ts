// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs

// Reference counting of shared structs and SharedArrays (REFCOUNT.md in github.com/arv/threads):
// every address is owned (+1), a stable borrow or an unstable borrow, and the emit retains and
// releases where the rules say.

// @filename: threads.d.mts
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

// @filename: list.mts
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
