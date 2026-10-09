//// [tests/cases/compiler/ownSharedThreads.ts] ////

//// [own.d.ts]
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

//// [nbody.ts]
type Body = { x: number; y: number; z: number; vx: number; vy: number; vz: number; m: number };
type Pos = { x: number; y: number; z: number; m: number };
declare const STEPS: number;
declare function makeBodies(): sending Body[];
declare function accelerate(mine: Body[], all: readonly shared Pos[]): void;
declare function move(mine: Body[]): void;
declare function energy(bodies: Body[]): number;

function worker(t: number, mine: sending Body[], board: Slots<shared Pos[]>, barrier: Barrier): sending Body[] {
  for (let s = 0; s < STEPS; s++) {
    board.set(t, share(mine.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m }))));
    barrier.wait();
    accelerate(mine, board.flat());
    barrier.wait();
    move(mine);
  }
  return mine;
}

function simulate(threads: number): number {
  const board = new Slots<shared Pos[]>(threads), barrier = new Barrier(threads);
  return scope(s => {
    const parts = partition(makeBodies(), threads);
    const ts = parts.map((part, t) => s.spawn(worker, t, consume part, board, barrier));
    return energy(ts.flatMap(th => th.join()));
  });
}

shared struct Counter { n: number; total: number }

//// [module.ts]
export shared struct Particle { readonly id: number; pos: shared Pos[]; mass: number }
type Pos = { x: number; y: number };

export function take(p: sending Particle, ps: readonly shared Pos[]): sending Particle[] {
    const out = [consume p];
    return ps.length > 0 ? out : (consume out);
}

// Contextual keywords keep their old meaning.
type shared<T> = T;
export const sending: shared<number>[] = [];
export function consume(struct: shared<number>): number { return struct; }
consume(sending[0]);


//// [nbody.js]
"use strict";
function worker(t, mine, board, barrier) {
    for (let s = 0; s < STEPS; s++) {
        board.set(t, share(mine.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m }))));
        barrier.wait();
        accelerate(mine, board.flat());
        barrier.wait();
        move(mine);
    }
    return mine;
}
function simulate(threads) {
    const board = new Slots(threads), barrier = new Barrier(threads);
    return scope(s => {
        const parts = partition(makeBodies(), threads);
        const ts = parts.map((part, t) => s.spawn(worker, t, part, board, barrier));
        return energy(ts.flatMap(th => th.join()));
    });
}
const Counter = __sharedStruct("Counter", ["n", "total"]);
//// [module.js]
export const Particle = __sharedStruct("Particle", ["id", "pos", "mass"]);
export function take(p, ps) {
    const out = [p];
    return ps.length > 0 ? out : out;
}
export const sending = [];
export function consume(struct) { return struct; }
consume(sending[0]);


//// [nbody.d.ts]
type Body = {
    x: number;
    y: number;
    z: number;
    vx: number;
    vy: number;
    vz: number;
    m: number;
};
type Pos = {
    x: number;
    y: number;
    z: number;
    m: number;
};
declare const STEPS: number;
declare function makeBodies(): sending Body[];
declare function accelerate(mine: Body[], all: readonly shared Pos[]): void;
declare function move(mine: Body[]): void;
declare function energy(bodies: Body[]): number;
declare function worker(t: number, mine: sending Body[], board: Slots<shared Pos[]>, barrier: Barrier): sending Body[];
declare function simulate(threads: number): number;
declare shared struct Counter {
    n: number;
    total: number;
}
//// [module.d.ts]
export declare shared struct Particle {
    readonly id: number;
    pos: shared Pos[];
    mass: number;
}
type Pos = {
    x: number;
    y: number;
};
export declare function take(p: sending Particle, ps: readonly shared Pos[]): sending Particle[];
type shared<T> = T;
export declare const sending: shared<number>[];
export declare function consume(struct: shared<number>): number;
export {};
