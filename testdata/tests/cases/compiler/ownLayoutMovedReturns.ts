// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./own.mjs

// A counted value that a callback returns to the runtime, whose function type has a `sending`
// result (a SharedMap's getOrInsertComputed's callback), is passed as `__moved(E)`: it carries its
// count, which the runtime takes over, and the number is an address. Another callback's result is
// not.

// @filename: own.d.mts
type Entry<K, V> = [key: K, value: V];
type Computed<K, V> = [key: K, callback: (key: K) => sending V];
/** A map in shared memory. */
export declare class SharedMap<K, V> {
    get(key: K): V | undefined;
    set(...entry: sending Entry<K, V>): this;
    getOrInsertComputed(...entry: sending Computed<K, V>): V;
}
/** A callback whose result is not sent. */
export declare function peek<V>(f: () => V): void;

// @filename: main.mts
import { SharedMap, peek } from "./own.mjs";

shared struct Count { n: number; }

export function count(counts: SharedMap<string, Count>, word: string) {
    counts.getOrInsertComputed(word, () => new Count()).n++;
    counts.getOrInsertComputed(word, function () { return new Count(); }).n++;
    counts.getOrInsertComputed(word, () => {
        const c = new Count();
        c.n = 1;
        return c;
    });
    counts.set(word, new Count());
    peek(() => new Count());
}
