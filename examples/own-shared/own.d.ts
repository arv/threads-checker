// Library surface for the "own and shared threads" examples.
//
// `sending T` marks a value whose ownership moves to the receiver: the sender must not use it
// afterwards (`consume x` hands over a local). `shared T` marks a deeply immutable value that any
// number of threads may read at the same time.

/** One slot per thread. A thread publishes into its own slot; everyone can read every slot. */
declare class Slots<T> {
  constructor(count: number);
  /** Publishes `value` in slot `index`, replacing what was there. */
  set(index: number, value: T): void;
  /** Gets the value in slot `index`. */
  get(index: number): T;
  /** Concatenates the arrays in all slots, in slot order. */
  flat(): T extends readonly (infer E)[] ? shared E[] : never;
}

/** A reusable barrier for a fixed number of threads. */
declare class Barrier {
  constructor(threads: number);
  /** Blocks until all threads have called `wait`. */
  wait(): void;
}

/** Freezes a value the caller owns into a deeply immutable, shareable value. */
declare function share<T>(value: sending T): shared T;

/** A thread started inside a `scope`; joining it hands back the thread's result. */
interface Thread<R> {
  join(): sending R;
}

/** Spawns threads that cannot outlive the scope. */
interface Scope {
  spawn<A extends unknown[], R>(fn: (...args: A) => sending R, ...args: A): Thread<R>;
}

/** Runs `body` and joins every thread it spawned before returning. */
declare function scope<R>(body: (s: Scope) => R): R;

/** Splits an owned array into `parts` owned, disjoint chunks of nearly equal size. */
declare function partition<T>(items: sending T[], parts: number): (sending T[])[];

/**
 * Placeholder runtime support for `shared struct Name { a: T; b: U }`, which currently compiles to
 * `const Name = __sharedStruct("Name", ["a", "b"])`. TODO: replace with a SharedArrayBuffer layout.
 */
declare function __sharedStruct(name: string, fields: readonly (string | number)[]): any;
