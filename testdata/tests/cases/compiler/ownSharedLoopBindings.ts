// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs
// @noEmit: true

// A variable declared in a loop's body is a fresh binding in every iteration: what an earlier
// iteration sent must not stick to it. A variable declared outside the loop keeps its state across
// iterations. Lines marked "error" are expected to be reported.

// @filename: threads.d.mts
// The part of the runtime's plain TypeScript API that this test uses.

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

// @filename: loops.mts
import { type Sending, type Shared, share } from "./threads.mjs";

type Pos = { x: number };
type Positions = { t: number; pos: Pos[] };
declare function send<T>(x: Sending<T>): void;
declare function recv(): Sending<Positions | undefined>;
declare function use(xs: Shared<Pos[]>): void;

// The n-body gather: each step builds `slots` from received messages, then shares it.
export function gather(steps: number, threads: number) {
    for (let s = 0; s < steps; s++) {
        const slots: Pos[][] = [];
        for (let k = 0; k < threads; k++) {
            const m = recv()!;
            slots[m.t] = m.pos;
        }
        const all = share(slots.flat());
        use(all);
    }
}

export function nested(steps: number) {
    for (let s = 0; s < steps; s++) {
        do {
            const { pos } = recv()!;          // destructured: fresh bindings too
            send(pos);
        } while (steps > s);
        let last: Pos[] = [];
        while (last.length < 3) {
            const next = recv()!.pos;
            last.push(...next);
        }
        send(last);
    }
}

export function outside(steps: number) {
    const slots: Pos[][] = [];
    for (let s = 0; s < steps; s++) {
        const all = share(slots.flat());      // error: sent in an earlier iteration
        use(all);
    }
}

export function linked(steps: number) {
    const outer: Pos[] = [];
    for (let s = 0; s < steps; s++) {
        const inner = outer;                  // error: a fresh binding, but `outer` was sent through it
        send(inner);                          // error: in an earlier iteration
    }
}

export function sameIteration(steps: number) {
    for (let s = 0; s < steps; s++) {
        const xs: Pos[] = [];
        send(xs);
        xs.push({ x: 1 });                    // error: sent above, in this iteration
    }
}
