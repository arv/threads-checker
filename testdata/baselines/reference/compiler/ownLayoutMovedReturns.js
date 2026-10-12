//// [tests/cases/compiler/ownLayoutMovedReturns.ts] ////

//// [own.d.mts]
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

//// [main.mts]
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


//// [main.mjs]
import { __F64, __moved, __newStruct, __ref, __register, __release, __sharedStruct } from "./own.mjs";
import { peek } from "./own.mjs";
const Count = __sharedStruct("Count", ["n"], []);
export function count(counts, word) {
    var _a;
    __F64[(counts.getOrInsertComputed(word, () => __moved(__newStruct(Count))) >> 3) + 1]++;
    __F64[(counts.getOrInsertComputed(word, function () { return __moved(__newStruct(Count)); }) >> 3) + 1]++;
    counts.getOrInsertComputed(word, () => {
        var _a;
        let c = __newStruct(Count);
        try {
            __F64[(c >> 3) + 1] = 1;
            return __moved((_a = c, c = 0, _a));
        }
        finally {
            __release(c);
        }
    });
    counts.set(word, __ref(_a = __newStruct(Count)));
    __release(_a);
    peek(() => __newStruct(Count));
}
__register(import.meta.url, { count });
