// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022,dom
// @ownRuntime: ./threads.mjs

// The n-body example as an ES module for the threads.mjs runtime: the ownership analysis accepts
// it, and the emit imports the runtime helpers it needs, lays out its shared struct, counts the
// struct's references and registers its top-level functions.

// @filename: threads.d.mts
// The parts of the runtime's API that the example uses. Programs import their API from
// "./threads.mjs"; the compiler adds the imports of the helpers it emits (`__sharedStruct`,
// `__register`, ...) when `ownRuntime` is set.

declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;

/** A cell: shared and mutable. Any number of threads may hold the same one. */
export interface Cell {
  readonly [cellBrand]: true;
}

/** A value whose ownership moves, on a parameter or a result. */
export type Sending<T> = T;

/** A deeply immutable value, which any number of threads may read. */
export type Shared<T> = T extends Cell | ((...args: any[]) => any) ? T
  : T extends object ? { readonly [K in keyof T]: Shared<T[K]> }
  : T;

/** Freezes a value the caller owns into a deeply immutable value every thread can read. */
export declare function share<T>(value: Sending<T>): Shared<T>;

/** A field of a shared struct. */
export interface Field<T> {
  readonly [fieldBrand]: T;
}
/** A number field: an f64 in shared memory. */
export declare const f64: Field<number>;

/** What `struct(...)` returns: `new S()` makes a zeroed struct in shared memory. */
export interface StructType<T> {
  new (): T & Cell;
  readonly fields: readonly string[];
}
/** The value type of a struct type: `type Body = Struct<typeof Body>`. */
export type Struct<S> = S extends StructType<infer T> ? T & Cell : never;

/** Declares a shared struct. */
export declare function struct<F extends Record<string, Field<any>>>(
  fields: F,
): StructType<{ -readonly [K in keyof F]: F[K] extends Field<infer V> ? V : never }>;

/** A thread; joining it moves the thread's result back. */
export interface Thread<R> {
  join(): Sending<R>;
}

/** Spawns threads that cannot outlive the scope. Every argument is sent to the new thread. */
export interface Scope {
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

/** Prints a line, synchronously, from any thread. */
export declare function log(...args: unknown[]): void;

// @filename: nbody.mts
// The n-body example as an ES module for the threads.mjs runtime. With `ownRuntime` set, the
// compiler imports the runtime helpers this module needs and registers its top-level functions, so
// that spawned threads can find `worker` by name.
import { Barrier, f64, log, partition, scope, type Sending, share, type Shared, Slots, struct, type Struct, take, type Thread } from "./threads.mjs";

type Body = { x: number; y: number; z: number; vx: number; vy: number; vz: number; m: number };
type Pos = { x: number; y: number; z: number; m: number };

const BODIES = 800, STEPS = 30, DT = 0.001, SOFTEN = 0.01;

/** Counts steps across threads. */
const Progress = struct({ steps: f64 });
type Progress = Struct<typeof Progress>;

function makeBodies(): Sending<Body[]> {
    let seed = 1;
    const random = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
    const bodies: Body[] = [];
    for (let i = 0; i < BODIES; i++) {
        bodies.push({ x: random() * 2 - 1, y: random() * 2 - 1, z: random() * 2 - 1, vx: 0, vy: 0, vz: 0, m: 0.5 + random() });
    }
    return bodies;
}

function accelerate(mine: Body[], all: Shared<Pos[]>) {
    for (const b of mine) {
        let ax = 0, ay = 0, az = 0;
        for (const o of all) {
            const dx = o.x - b.x, dy = o.y - b.y, dz = o.z - b.z;
            const d2 = dx * dx + dy * dy + dz * dz + SOFTEN;
            const f = o.m / (d2 * Math.sqrt(d2));
            ax += dx * f;
            ay += dy * f;
            az += dz * f;
        }
        b.vx += ax * DT;
        b.vy += ay * DT;
        b.vz += az * DT;
    }
}

function move(mine: Body[]) {
    for (const b of mine) {
        b.x += b.vx * DT;
        b.y += b.vy * DT;
        b.z += b.vz * DT;
    }
}

function energy(bodies: Body[]): number {
    let e = 0;
    for (const b of bodies) e += 0.5 * b.m * (b.vx * b.vx + b.vy * b.vy + b.vz * b.vz);
    return e;
}

function worker(t: number, mine: Sending<Body[]>, board: Slots<Pos[]>, barrier: Barrier, progress: Progress): Sending<Body[]> {
    for (let s = 0; s < STEPS; s++) {
        board.set(t, share(mine.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m }))));
        barrier.wait();
        accelerate(mine, board.flat());
        barrier.wait();
        move(mine);
        progress.steps++;
    }
    return mine;
}

function simulate(threads: number): number {
    const board = new Slots<Pos[]>(threads), barrier = new Barrier(threads), progress = new Progress();
    return scope(s => {
        const parts = partition(makeBodies(), threads);
        const ts: Thread<Body[]>[] = [];
        for (let t = 0; t < threads; t++) ts.push(s.spawn(worker, t, take(parts, t), board, barrier, progress));
        return energy(ts.flatMap(th => th.join()));
    });
}

export function main(threadCounts: readonly number[]) {
    for (const threads of threadCounts) {
        log(`threads ${threads}  kinetic energy ${simulate(threads).toFixed(6)}`);
    }
}
