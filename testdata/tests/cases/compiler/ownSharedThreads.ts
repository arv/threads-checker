// @target: es2022
// @module: es2022
// @strict: true
// @lib: es2022
// @declaration: true

// Syntax of the "own and shared threads" extension: `sending T`, `shared T`, `consume x` and
// `shared struct`. The types are erased, `consume x` emits as `x`, and a shared struct emits as
// a placeholder `__sharedStruct` call. The ownership analysis accepts the program.

// @filename: own.d.ts
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

// @filename: nbody.ts
type Body = { x: number; y: number; z: number; vx: number; vy: number; vz: number; m: number };
type Pos = { x: number; y: number; z: number; m: number };
declare const STEPS: number;
declare function makeBodies(): sending Body[];
declare function accelerate(mine: Body[], all: readonly shared Pos[]): void;
declare function move(mine: Body[]): void;
declare function energy(bodies: Body[]): number;

function worker(t: number, mine: sending Body[], board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell): sending Body[] {
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
  const board = new Slots<Pos[]>(threads), barrier = new Barrier(threads);
  return scope(s => {
    const parts = partition(makeBodies(), threads);
    const ts: (shared Thread<Body[]>)[] = [];
    for (let t = 0; t < threads; t++) {
      ts.push(s.spawn(worker, t, take(parts, t), board, barrier));
    }
    return energy(ts.flatMap(th => th.join()));
  });
}

shared struct Counter { n: number; total: number }

// @filename: module.ts
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
