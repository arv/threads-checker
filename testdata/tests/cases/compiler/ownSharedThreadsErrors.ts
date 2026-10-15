// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs
// @noEmit: true

// The ownership analysis on the n-body example: one wrong program per kind of error, and correct
// programs (nbody.mts and `fine`).

// @filename: threads.d.mts
// The part of the runtime's plain TypeScript API that the n-body example uses.

declare const cellBrand: unique symbol;

/** A cell: shared and mutable. Any number of threads may hold the same one. */
export interface Cell {
  readonly [cellBrand]: true;
}

/** A value whose ownership moves, on a parameter or a result. To TypeScript it is just T. */
export type Sending<T> = T;

/** A deeply immutable value, which any number of threads may read. To TypeScript it is deeply readonly. */
export type Shared<T> = T extends Cell | ((...args: any[]) => any) ? T
  : T extends object ? { readonly [K in keyof T]: Shared<T[K]> }
  : T;

/** Freezes a value the caller owns into a deeply immutable value every thread can read. */
export declare function share<T>(value: Sending<T>): Shared<T>;

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

// @filename: bodies.d.mts
import type { Sending, Shared } from "./threads.mjs";

export type Body = { x: number; y: number; z: number; vx: number; vy: number; vz: number; m: number };
export type Pos = { x: number; y: number; z: number; m: number };
export declare const STEPS: number;
export declare function makeBodies(): Sending<Body[]>;
export declare function accelerate(mine: Body[], all: Shared<Pos[]>): void;
export declare function move(mine: Body[]): void;
export declare function energy(bodies: Body[]): number;

// @filename: nbody.mts
import { Barrier, partition, scope, type Sending, share, Slots, take, type Thread } from "./threads.mjs";
import { accelerate, type Body, energy, makeBodies, move, type Pos, STEPS } from "./bodies.mjs";

export function worker(t: number, mine: Sending<Body[]>, board: Slots<Pos[]>, barrier: Barrier): Sending<Body[]> {
  for (let s = 0; s < STEPS; s++) {
    board.set(t, share(mine.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m }))));
    barrier.wait();
    accelerate(mine, board.flat());
    barrier.wait();
    move(mine);
  }
  return mine;
}

export function simulate(threads: number): number {
  const board = new Slots<Pos[]>(threads), barrier = new Barrier(threads);
  return scope(s => {
    const parts = partition(makeBodies(), threads);
    const ts: Thread<Body[]>[] = [];
    for (let t = 0; t < threads; t++) {
      ts.push(s.spawn(worker, t, take(parts, t), board, barrier));
    }
    return energy(ts.flatMap(th => th.join()));
  });
}

// @filename: errors.mts
// Programs that the ownership analysis rejects, one per kind of error, and one that it accepts.
// Writes to shared values cast the readonly type away, since TypeScript already rejects them
// otherwise: the analysis rejects them anyway.
import { type Barrier, type Scope, type Sending, share, type Slots } from "./threads.mjs";
import { accelerate, type Body, makeBodies, move, type Pos } from "./bodies.mjs";
import { worker } from "./nbody.mjs";

// 1. Use after send, through another variable that shares objects with the sent one.
function sendHalf(bodies: Sending<Body[]>, s: Scope, board: Slots<Pos[]>, barrier: Barrier) {
  const half = bodies.slice(0, 400);
  s.spawn(worker, 0, half, board, barrier);   // half shares bodies' region: sends it
  bodies[0].x = 1;                            // error: 'bodies' shares objects with 'half', which was sent
}

// 1. Use after consume: share() turns its argument into a shared value.
function snapshot(snap: Sending<Pos[]>) {
  const p = share(snap);
  snap[0].x = 2;                              // error: 'snap' was consumed by 'share'
  // 4. Writes through a shared value.
  (p as Pos[])[0].x = 3;                      // error: p[0] is shared
  (p as Pos[]).push({ x: 0, y: 0, z: 0, m: 0 }); // error: p is shared
}

// 1. Sent in one iteration, used in the next.
function spawnInLoop(s: Scope, board: Slots<Pos[]>, barrier: Barrier) {
  const mine = makeBodies();
  for (let t = 0; t < 4; t++) {
    s.spawn(worker, t, mine, board, barrier); // error on the second iteration: 'mine' may have been sent
  }
}

// 2. Sending a value the caller still references.
function sendParameter(xs: Body[], s: Scope, board: Slots<Pos[]>, barrier: Barrier) {
  s.spawn(worker, 0, xs, board, barrier);     // error: declare xs 'Sending<T>'
}

// 2. Sending a value captured from an outer scope.
const everyone = makeBodies();
function sendCaptured(s: Scope, board: Slots<Pos[]>, barrier: Barrier) {
  s.spawn(worker, 0, everyone, board, barrier); // error: captured
}

// 3. Returning a caller's value as `Sending<T>`.
function giveBack(xs: Body[]): Sending<Body[]> {
  return xs;                                  // error: the caller still references xs
}

// No errors: a fresh literal is sent, a map builds fresh objects that are then shared, and a value
// is only used before it is sent.
function fine(bodies: Body[], s: Scope, board: Slots<Pos[]>, barrier: Barrier) {
  s.spawn(worker, 0, [{ x: 0, y: 0, z: 0, vx: 0, vy: 0, vz: 0, m: 1 }], board, barrier);
  const positions = share(bodies.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m })));
  accelerate(bodies, positions);
  const before = makeBodies();
  move(before);
  s.spawn(worker, 1, before, board, barrier);
}
