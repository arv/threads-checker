// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs
// @noEmit: true

// Control flow, closures and the map rule of the ownership analysis. Lines marked "error" are
// expected to be reported. Writes to shared values cast the readonly type away, since TypeScript
// already rejects them otherwise: the analysis rejects them anyway.

// @filename: threads.d.mts
// The part of the runtime's plain TypeScript API that this test uses.

declare const cellBrand: unique symbol;
declare const fieldBrand: unique symbol;

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

/** Ends `x`'s lifetime: its value, and what it reaches, moves to the caller. At run time, `x` itself. */
export declare function consume<T>(x: T): Sending<T>;

/** A field of a shared struct, declared with `f64` or `ref<T>()`. */
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

// @filename: flow.mts
import { consume, f64, type Sending, type Shared, struct, type Struct } from "./threads.mjs";

type Box = { v: number; next?: Box };
declare function send<T>(x: Sending<T>): void;
declare function share<T>(x: Sending<T>): Shared<T>;
declare function make(): Sending<Box>;
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

export function destructuring(o: Sending<{ p: Box; q: Box }>) {
    const { p, q } = o;
    send(p);
    use(q);                                   // error: q shares objects with p
}

export function consumes(x: Sending<Box>) {
    const y = consume(x);
    use(y);
    use(x);                                   // error: consumed
    send(consume(y));
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

export function sharedValues(p: Shared<Box[]>, q: Shared<readonly Box[]>) {
    (p as Box[])[0].v = 1;                    // error
    (p as Box[])[0].v++;                      // error
    delete (p as Box[])[0].next;              // error
    (p as Box[]).sort();                      // error
    use(q.length + Math.max(1, 2));
    const s = share({ v: 1 });
    (s as Box).v = 2;                         // error
    let u: Box[] | undefined;
    for (const x of q) { if (u) u.push(x); else u = []; } // fine: u is not a shared value
}

// A shared struct's fields are cells, but its values are counted: sending one moves it, so a
// parameter or a captured struct cannot be sent, like any other value.
const Counter = struct({ n: f64 });
type Counter = Struct<typeof Counter>;
export function structs(c: Counter) {
    c.n++;                                    // fine: struct fields are cells
    send(c);                                  // error: c is the caller's
    const d = new Counter();
    [1].forEach(() => send(d));               // error: d is captured
    const e = new Counter();
    send(e);                                  // fine: a new instance is the function's own
}

export const giveBack = (x: Box): Sending<Box> => x; // error: x is the caller's
export const fresh = (): Sending<Box> => ({ v: 1 });

const top = make();
send(top);
use(top);                                     // error: top-level code is analyzed too
