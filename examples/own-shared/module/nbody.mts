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
