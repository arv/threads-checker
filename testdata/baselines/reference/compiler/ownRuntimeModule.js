//// [tests/cases/compiler/ownRuntimeModule.ts] ////

//// [own.d.mts]
// Types for the own/shared threads runtime own.mjs (now own.ts in github.com/arv/threads). Programs
// import their API from "./own.mjs"; the compiler adds the imports of the helpers it emits
// (`__sharedStruct`, `__register`) when `ownRuntime` is set.
//
// `sending T` marks a value whose ownership moves to the receiver: the sender must not use it
// afterwards. `shared T` marks a value any number of threads may read at once: deeply immutable, or
// a cell that synchronizes internally (Slots, Barrier, a shared struct).

/** One slot per thread: a thread publishes into its own slot, and everyone reads every slot. */
export interface SlotsCell<T> {
    readonly length: number;
    /** Publishes `value` in slot `index`, replacing what was there. */
    set(index: number, value: T): void;
    /** The value in slot `index`. */
    get(index: number): shared T;
    /** The arrays in all slots, concatenated in slot order. */
    flat(): shared (T extends readonly (infer E)[] ? E : T)[];
}
export declare const Slots: new <T>(count: number) => shared SlotsCell<T>;

/** A reusable barrier for a fixed number of threads. */
export interface BarrierCell {
    /** Blocks until all threads have called `wait`. */
    wait(): void;
}
export declare const Barrier: new (threads: number) => shared BarrierCell;

/** Freezes a value the caller owns into a deeply immutable value every thread can read. */
export declare function share<T>(value: sending T): shared T;

/** A thread started inside a `scope`; joining it moves the thread's result back. */
export interface Thread<R> {
    join(): sending R;
}

/** Spawns threads that cannot outlive the scope. Every argument is sent to the new thread. */
export interface Scope {
    spawn<A extends unknown[], R>(f: (...args: A) => sending R, ...args: sending A): shared Thread<R>;
}

/** Runs `body` and joins every thread it spawned before returning. */
export declare function scope<R>(body: (s: shared Scope) => R): R;

/** Splits an owned array into `parts` owned, disjoint chunks of nearly equal size. Trusted. */
export declare function partition<T>(items: sending T[], parts: number): (sending T[])[];

/** Takes element `index` out of an array of owned values, so the caller owns it. Trusted. */
export declare function take<T>(items: (sending T)[], index: number): sending T;

/** Prints a line, synchronously, from any thread. */
export declare function log(...args: unknown[]): void;

/** A shared struct's value as a type: what `new SharedArray(Struct, n)` takes. */
export interface SharedStructType<T> {
    new (): T;
}

/**
 * A fixed-length array in the shared heap, of numbers or of shared structs stored inline. Threads
 * share it with no copying. With `ownRuntime` the compiler lays it out in the shared heap and turns
 * `a[i]` and `a.length` into typed-array accesses.
 */
export interface SharedArray<T> {
    readonly length: number;
    [index: number]: T;
}
export declare const SharedArray: {
    new (type: NumberConstructor, length: number): SharedArray<number>;
    new <T>(type: SharedStructType<T>, length: number): SharedArray<T>;
};

//// [nbody.mts]
// The n-body example as an ES module for the own.mjs runtime. With `ownRuntime` set, the compiler
// imports the runtime helpers this module needs and registers its top-level functions, so that
// spawned threads can find `worker` by name.
import { Barrier, type BarrierCell, log, partition, scope, share, Slots, type SlotsCell, take, type Thread } from "./own.mjs";

type Body = { x: number; y: number; z: number; vx: number; vy: number; vz: number; m: number };
type Pos = { x: number; y: number; z: number; m: number };

const BODIES = 800, STEPS = 30, DT = 0.001, SOFTEN = 0.01;

/** Counts steps across threads. */
shared struct Progress { steps: number }

function makeBodies(): sending Body[] {
    let seed = 1;
    const random = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
    const bodies: Body[] = [];
    for (let i = 0; i < BODIES; i++) {
        bodies.push({ x: random() * 2 - 1, y: random() * 2 - 1, z: random() * 2 - 1, vx: 0, vy: 0, vz: 0, m: 0.5 + random() });
    }
    return bodies;
}

function accelerate(mine: Body[], all: readonly shared Pos[]) {
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

function worker(t: number, mine: sending Body[], board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell, progress: Progress): sending Body[] {
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
        const ts: (shared Thread<Body[]>)[] = [];
        for (let t = 0; t < threads; t++) ts.push(s.spawn(worker, t, take(parts, t), board, barrier, progress));
        return energy(ts.flatMap(th => th.join()));
    });
}

export function main(threadCounts: readonly number[]) {
    for (const threads of threadCounts) {
        log(`threads ${threads}  kinetic energy ${simulate(threads).toFixed(6)}`);
    }
}


//// [nbody.mjs]
import { __F64, __hold, __newStruct, __ref, __register, __release, __sharedStruct } from "./own.mjs";
// The n-body example as an ES module for the own.mjs runtime. With `ownRuntime` set, the compiler
// imports the runtime helpers this module needs and registers its top-level functions, so that
// spawned threads can find `worker` by name.
import { Barrier, log, partition, scope, share, Slots, take } from "./own.mjs";
const BODIES = 800, STEPS = 30, DT = 0.001, SOFTEN = 0.01;
/** Counts steps across threads. */
const Progress = __sharedStruct("Progress", ["steps"], []);
function makeBodies() {
    let seed = 1;
    const random = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
    const bodies = [];
    for (let i = 0; i < BODIES; i++) {
        bodies.push({ x: random() * 2 - 1, y: random() * 2 - 1, z: random() * 2 - 1, vx: 0, vy: 0, vz: 0, m: 0.5 + random() });
    }
    return bodies;
}
function accelerate(mine, all) {
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
function move(mine) {
    for (const b of mine) {
        b.x += b.vx * DT;
        b.y += b.vy * DT;
        b.z += b.vz * DT;
    }
}
function energy(bodies) {
    let e = 0;
    for (const b of bodies)
        e += 0.5 * b.m * (b.vx * b.vx + b.vy * b.vy + b.vz * b.vz);
    return e;
}
function worker(t, mine, board, barrier, progress) {
    for (let s = 0; s < STEPS; s++) {
        board.set(t, share(mine.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m }))));
        barrier.wait();
        accelerate(mine, board.flat());
        barrier.wait();
        move(mine);
        __F64[(progress >> 3) + 1]++;
    }
    return mine;
}
function simulate(threads) {
    const board = new Slots(threads), barrier = new Barrier(threads), progress = __newStruct(Progress);
    try {
        return scope(__hold(s => {
            const parts = partition(makeBodies(), threads);
            const ts = [];
            for (let t = 0; t < threads; t++)
                ts.push(s.spawn(worker, t, take(parts, t), board, barrier, __ref(progress)));
            return energy(ts.flatMap(th => th.join()));
        }, progress));
    }
    finally {
        __release(progress);
    }
}
export function main(threadCounts) {
    for (const threads of threadCounts) {
        log(`threads ${threads}  kinetic energy ${simulate(threads).toFixed(6)}`);
    }
}
__register(import.meta.url, { makeBodies, accelerate, move, energy, worker, simulate, main });
