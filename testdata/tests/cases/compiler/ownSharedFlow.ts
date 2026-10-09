// @target: es2022
// @module: es2022
// @strict: true
// @lib: es2022
// @noEmit: true

// Control flow, closures and the map rule of the ownership analysis of the "own and shared
// threads" extension. Lines marked "error" are expected to be reported.

type Box = { v: number; next?: Box };
declare function send<T>(x: sending T): void;
declare function share<T>(x: sending T): shared T;
declare function make(): sending Box;
declare function use(x: unknown): void;
declare const flag: boolean;

export function joins() {
    const a = make();
    if (flag) { send(a); }
    use(a);                                   // error: may have been sent
    const b = make();
    if (flag) { send(b); } else { send(b); }
    use(b);                                   // error: sent on both paths
    const c = make(), d = make();
    const e = flag ? c : d;
    send(e);
    use(d);                                   // error: d shares objects with e
    const f = make();
    flag && send(f);
    use(f);                                   // error: may have been sent
}

export function switches(k: number) {
    const a = make();
    switch (k) {
        case 0: send(a); break;
        case 1: use(a);
        default: break;
    }
    use(a);                                   // error: may have been sent
}

export function loops() {
    const a = make();
    for (let i = 0; i < 3; i++) {
        use(a);                               // error: sent in the previous iteration
        send(a);
    }
    for (let i = 0; i < 3; i++) {
        const fresh = make();
        send(fresh);                          // fine: a new value every iteration
    }
    const b = make();
    outer: while (flag) {
        while (flag) {
            if (flag) break outer;
            continue outer;
        }
    }
    send(b);                                  // fine
    for (const x of [make()]) send(x);        // error on the next iteration: elements share the array's region
}

export function tries() {
    const a = make();
    try { send(a); } catch (e) { send(e); } finally { use(a); } // errors: e is captured, a may have been sent
}

export function destructuring(o: sending ({ p: Box; q: Box })) {
    const { p, q } = o;
    send(p);
    use(q);                                   // error: q shares objects with p
}

export function consumes(x: sending Box) {
    const y = consume x;
    use(y);
    use(x);                                   // error: consumed
    send(consume y);
    use(y);                                   // error: consumed by send
}

export class C {
    f: Box = { v: 1 };
    m() { send(this.f); }                     // error: this is the caller's
    n = () => send(this.f);                   // error: this is captured
}

export function closures() {
    const a = make();
    const f = () => a.v;
    send(f);
    use(a);                                   // error: a shares objects with f
    const c = make();
    [1, 2].forEach(i => { send(c); });        // error: c is captured
}

export function maps(xs: Box[]) {
    const fresh = xs.map(x => ({ v: x.v }));
    send(fresh);                              // fine: the callback builds fresh objects
    const nums = xs.map(x => x.v);
    send(nums);                               // fine
    const aliased = xs.map(x => x);
    send(aliased);                            // error: holds the caller's objects
    const filtered = xs.filter(x => x.v > 0);
    send(filtered);                           // error: filter aliases the receiver
}

export function sharedValues(p: shared Box[], q: readonly shared Box[]) {
    p[0].v = 1;                               // error
    p[0].v++;                                 // error
    delete p[0].next;                         // error
    p.sort();                                 // error
    use(q.length + Math.max(1, 2));
    const s = share({ v: 1 });
    s.v = 2;                                  // error
    let u: Box[] | undefined;
    for (const x of q) { if (u) u.push(x); else u = []; } // fine: u is not a shared value
}

export shared struct Counter { n: number }
export function structs(c: Counter) {
    c.n++;                                    // fine: struct fields are cells
    send(c);                                  // fine: shared structs are shared
}

export const giveBack = (x: Box): sending Box => x; // error: x is the caller's
export const fresh = (): sending Box => ({ v: 1 });

const top = make();
send(top);
use(top);                                     // error: top-level code is analyzed too
