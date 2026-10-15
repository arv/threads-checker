// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs
// @noEmit: true

// Errors of reference counting: what the emit cannot count.

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

// @filename: errors.mts
import { f64, ref, struct } from "./threads.mjs";

export interface Node { value: number; next: Node | null }
export const Node = struct<Node>({ value: f64, next: ref<Node>() });

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
