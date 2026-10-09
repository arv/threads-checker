// Library surface for the "own and shared threads" examples.
//
// `sending T` marks a value whose ownership moves to the receiver: the sender must not use it
// afterwards. `shared T` marks a deeply immutable value that any number of threads may read at the
// same time. Cells such as Slots and Barrier are created shared: they synchronize internally, so all
// threads may call their methods.

/** One slot per thread. A thread publishes into its own slot; everyone can read every slot. */
interface SlotsCell<T> {
    /** Publishes `value` in slot `index`, replacing what was there. */
    set(index: number, value: T): void;
    /** Gets the value in slot `index`. Values published in a shared cell are shared. */
    get(index: number): shared T;
    /** Concatenates the arrays in all slots, in slot order. */
    flat(): shared (T extends readonly (infer E)[] ? E : T)[];
}
declare const Slots: new <T>(count: number) => shared SlotsCell<T>;

/** A reusable barrier for a fixed number of threads. */
interface BarrierCell {
    /** Blocks until all threads have called `wait`. */
    wait(): void;
}
declare const Barrier: new (threads: number) => shared BarrierCell;

/** Freezes a value the caller owns into a deeply immutable, shareable value. */
declare function share<T>(value: sending T): shared T;

/** A thread started inside a `scope`; joining it hands back the thread's result. */
interface Thread<R> {
    join(): sending R;
}

/** Spawns threads that cannot outlive the scope. Every argument is sent to the new thread. */
interface Scope {
    spawn<A extends unknown[], R>(f: (...args: A) => sending R, ...args: sending A): shared Thread<R>;
}

/** Runs `body` and joins every thread it spawned before returning. */
declare function scope<R>(body: (s: shared Scope) => R): R;

/** Splits an owned array into `parts` owned, disjoint chunks of nearly equal size. */
declare function partition<T>(items: sending T[], parts: number): (sending T[])[];

/**
 * Takes element `index` out of an array of owned values (a destructive read: the element leaves the
 * array), so the caller owns it. Trusted: the analysis believes the `sending` return type.
 */
declare function take<T>(items: (sending T)[], index: number): sending T;

/**
 * Placeholder runtime support for `shared struct Name { a: T; b: U }`, which currently compiles to
 * `const Name = __sharedStruct("Name", ["a", "b"])`. TODO: replace with a SharedArrayBuffer layout.
 */
declare function __sharedStruct(name: string, fields: readonly (string | number)[]): any;
