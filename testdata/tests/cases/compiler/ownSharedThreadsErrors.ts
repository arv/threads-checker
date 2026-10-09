// @target: es2022
// @module: es2022
// @strict: true
// @lib: es2022
// @noEmit: true

// The ownership analysis of the "own and shared threads" extension: one wrong program per
// kind of error, and one correct program.

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

// @filename: errors.ts
// Programs that the ownership analysis rejects, one per kind of error, and one that it accepts.
// Compile with `tsgo -p tsconfig.errors.json`; nbody.ts provides `worker`, `Body` and `Pos`.

// 1. Use after send, through another variable that shares objects with the sent one.
function sendHalf(bodies: sending Body[], s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  const half = bodies.slice(0, 400);
  s.spawn(worker, 0, half, board, barrier);   // half shares bodies' region: sends it
  bodies[0].x = 1;                            // error: 'bodies' shares objects with 'half', which was sent
}

// 1. Use after consume: share() turns its argument into a shared value.
function snapshot(snap: sending Pos[]) {
  const p = share(snap);
  snap[0].x = 2;                              // error: 'snap' was consumed by 'share'
  // 4. Writes through a shared value.
  p[0].x = 3;                                 // error: p[0] is shared
  p.push({ x: 0, y: 0, z: 0, m: 0 });         // error: p is shared
}

// 1. Sent in one iteration, used in the next.
function spawnInLoop(s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  const mine = makeBodies();
  for (let t = 0; t < 4; t++) {
    s.spawn(worker, t, mine, board, barrier); // error on the second iteration: 'mine' may have been sent
  }
}

// 2. Sending a value the caller still references.
function sendParameter(xs: Body[], s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  s.spawn(worker, 0, xs, board, barrier);     // error: declare xs 'sending'
}

// 2. Sending a value captured from an outer scope.
const everyone = makeBodies();
function sendCaptured(s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  s.spawn(worker, 0, everyone, board, barrier); // error: captured
}

// 3. Returning a caller's value as `sending`.
function giveBack(xs: Body[]): sending Body[] {
  return xs;                                  // error: the caller still references xs
}

// No errors: a fresh literal is sent, a map builds fresh objects that are then shared, and a value
// is only used before it is sent.
function fine(bodies: Body[], s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  s.spawn(worker, 0, [{ x: 0, y: 0, z: 0, vx: 0, vy: 0, vz: 0, m: 1 }], board, barrier);
  const positions = share(bodies.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m })));
  accelerate(bodies, positions);
  const before = makeBodies();
  move(before);
  s.spawn(worker, 1, before, board, barrier);
}
