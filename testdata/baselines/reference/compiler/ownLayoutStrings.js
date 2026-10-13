//// [tests/cases/compiler/ownLayoutStrings.ts] ////

//// [threads.d.mts]
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

/** A field of a shared struct, declared with `f64`, `bool`, `str` or `ref<T>()`. */
export interface Field<T> {
  readonly [fieldBrand]: T;
}
/** A number field: an f64 in shared memory. */
export declare const f64: Field<number>;
/** A boolean field: a byte in shared memory, 0 or 1. */
export declare const bool: Field<boolean>;
/** A string field: the address of the string's one interned copy in shared memory. */
export declare const str: Field<string>;
/** A reference field: a shared struct or a SharedArray, or null. */
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
 * A fixed-length array in shared memory, of numbers or of shared structs stored inline. Every thread
 * reads and writes the same elements, with no copying; a Barrier or a Channel orders them.
 */
export interface SharedArray<T> extends Cell {
  readonly length: number;
  [index: number]: T;
}
export declare const SharedArray: {
  new (type: NumberConstructor, length: number): SharedArray<number>;
  new (type: BooleanConstructor, length: number): SharedArray<boolean>;
  new (type: StringConstructor, length: number): SharedArray<string>;
  new <T>(type: StructType<T>, length: number): SharedArray<T & Cell>;
};

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

//// [strings.mts]
import { f64, ref, SharedArray, str, struct } from "./threads.mjs";

interface Person { name: string; age: number; next: Person | null; city: string }
const Person = struct<Person>({ name: str, age: f64, next: ref<Person>(), city: str });

export function greet(p: Person): string {
    p.name = "Ada";                                   // its value is not used: no temporary
    const said = (p.city = "London");                 // used: the string, not its address
    if (p.next && p.next.name === "") p.next.name = p.name;
    p.name += "!";
    return `${p.name} from ${said}, ${p.age}`;
}

export function defaults(p: Person, city: string): string {
    const a = (p.city ||= city);
    const b = (p.name &&= p.name.toUpperCase());
    p.city ??= city;                                  // a string is never null: only read
    return a + b;
}

export function labels(n: number): number {
    const names = new SharedArray(String, n);
    for (let i = 0; i < names.length; i++) names[i] = i % 2 ? "odd" : "even";   // in bounds: no check
    names[n - 1] += "?";
    let chars = 0;
    for (let i = 0; i < n; i++) chars += names[i].length;
    return chars + names.length;
}

//// [syntax.mts]
// The syntax's shared structs take string fields as well, string literal types included.
shared struct Task { id: number; state: "todo" | "done"; title: string }
export function finish(t: Task): string {
    t.state = "done";
    return t.title + ": " + t.state;
}


//// [strings.mjs]
import { __F64, __I32, __OOB, __newSharedArray, register as __register, __release, __sharedStruct, __storeStr, __str } from "./threads.mjs";
import { f64, ref, SharedArray, str, struct } from "./threads.mjs";
const Person = __sharedStruct("Person", ["name", "age", "next", "city"], [2], "sfrs");
export function greet(p) {
    var _a;
    __storeStr((p >> 2) + 2, "Ada"); // its value is not used: no temporary
    const said = (__storeStr((p >> 2) + 8, "London")); // used: the string, not its address
    if (__I32[(p >> 2) + 6] && __str(__I32[(__I32[(p >> 2) + 6] >> 2) + 2]) === "")
        __storeStr((__I32[(p >> 2) + 6] >> 2) + 2, __str(__I32[(p >> 2) + 2]));
    _a = (p >> 2) + 2, __storeStr(_a, __str(__I32[_a]) + "!");
    return `${__str(__I32[(p >> 2) + 2])} from ${said}, ${__F64[(p >> 3) + 2]}`;
}
export function defaults(p, city) {
    var _a, _b, _c;
    const a = (_a = (p >> 2) + 8, __str(__I32[_a]) || (__storeStr(_a, city)));
    const b = (_b = (p >> 2) + 2, __str(__I32[_b]) && (__storeStr(_b, __str(__I32[(p >> 2) + 2]).toUpperCase())));
    _c = (p >> 2) + 8, __str(__I32[_c]); // a string is never null: only read
    return a + b;
}
export function labels(n) {
    var _a, _b;
    const names = __newSharedArray(4, n);
    try {
        for (let i = 0; i < __I32[(names + 4) >> 2]; i++)
            __storeStr(((names + 16) >> 2) + i, i % 2 ? "odd" : "even"); // in bounds: no check
        _b = (_a = n - 1, ((names + 16) >> 2) + ((_a >>> 0) === _a && _a < __I32[(names + 4) >> 2] ? _a : __OOB[_a])), __storeStr(_b, __str(__I32[_b]) + "?");
        let chars = 0;
        for (let i = 0; i < n; i++)
            chars += ((i >>> 0) === i && i < __I32[(names + 4) >> 2] ? __str(__I32[((names + 16) >> 2) + i]) : __OOB[i]).length;
        return chars + __I32[(names + 4) >> 2];
    }
    finally {
        __release(names);
    }
}
__register(import.meta.url, { greet, defaults, labels });
//// [syntax.mjs]
import { __I32, register as __register, __sharedStruct, __storeStr, __str } from "./threads.mjs";
// The syntax's shared structs take string fields as well, string literal types included.
const Task = __sharedStruct("Task", ["id", "state", "title"], [], "fss");
export function finish(t) {
    __storeStr((t >> 2) + 4, "done");
    return __str(__I32[(t >> 2) + 6]) + ": " + __str(__I32[(t >> 2) + 4]);
}
__register(import.meta.url, { finish });
