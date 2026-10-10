// @target: es2022
// @module: es2022
// @strict: true
// @lib: es2022
// @noEmit: true

// A variable declared in a loop's body is a fresh binding in every iteration: what an earlier
// iteration sent must not stick to it. A variable declared outside the loop keeps its state across
// iterations. Lines marked "error" are expected to be reported.

type Pos = { x: number };
type Positions = { t: number; pos: Pos[] };
declare function share<T>(x: sending T): shared T;
declare function send<T>(x: sending T): void;
declare function recv(): sending Positions | undefined;
declare function use(xs: readonly shared Pos[]): void;

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
