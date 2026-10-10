currentDirectory::/home/src/workspaces/project
useCaseSensitiveFileNames::true
Input::
//// [/home/src/workspaces/project/main.mts] *new* 
import { Channel, type Shared, share } from "./threads.mjs";
type Job = { data: number[] };
export function useAfterSend(ch: Channel<Job>) {
  const job: Job = { data: [] };
  ch.send(job);
  job.data.push(1);
}
export function writeShared() {
  const table: Shared<number[]> = share([1]);
  (table as number[]).push(2);
}
const notANumber: number = "a type error, which is tsc's to report";
//// [/home/src/workspaces/project/threads.d.mts] *new* 
declare const cellBrand: unique symbol;
export interface Cell { readonly [cellBrand]: true }
export type Sending<T> = T;
export type Shared<T> = T extends Cell | ((...args: any[]) => any) ? T
  : T extends object ? { readonly [K in keyof T]: Shared<T[K]> }
  : T;
export declare function consume<T>(x: T): Sending<T>;
export declare function share<T>(value: Sending<T>): Shared<T>;
export declare class Channel<T> implements Cell {
  readonly [cellBrand]: true;
  constructor(capacity?: number);
  send(value: Sending<T>): void;
  recv(): Sending<T> | undefined;
}
//// [/home/src/workspaces/project/tsconfig.json] *new* 
{
  "compilerOptions": { "target": "es2022", "module": "esnext", "moduleResolution": "bundler", "strict": true, "noEmit": true },
  "files": ["main.mts"]
}

tsgo own check -p tsconfig.json --ownRuntime ./threads.mjs
ExitStatus:: DiagnosticsPresent_OutputsSkipped
Output::
[96mmain.mts[0m:[93m6[0m:[93m3[0m - [91merror[0m[90m TS96001: [0m'job' cannot be used here: it was sent to 'send' on line 5.

[7m6[0m   job.data.push(1);
[7m [0m [91m  ~~~[0m

  [96mmain.mts[0m:[93m5[0m:[93m11[0m - 'job' is sent here.
    [7m5[0m   ch.send(job);
    [7m [0m [96m          ~~~[0m

[96mmain.mts[0m:[93m10[0m:[93m3[0m - [91merror[0m[90m TS96010: [0mCannot change 'table as number[]': it is a shared value, which is immutable.

[7m10[0m   (table as number[]).push(2);
[7m  [0m [91m  ~~~~~~~~~~~~~~~~~~~~~~~~[0m


Found 2 errors in the same file, starting at: main.mts[90m:6[0m

//// [/home/src/tslibs/TS/Lib/lib.es2022.full.d.ts] *Lib*
/// <reference no-default-lib="true"/>
interface Boolean {}
interface Function {}
interface CallableFunction {}
interface NewableFunction {}
interface IArguments {}
interface Number { toExponential: any; }
interface Object {}
interface RegExp {}
interface String { charAt: any; }
interface Array<T> { length: number; [n: number]: T; }
interface ReadonlyArray<T> {}
interface SymbolConstructor {
    (desc?: string | number): symbol;
    for(name: string): symbol;
    readonly toStringTag: symbol;
}
declare var Symbol: SymbolConstructor;
interface Symbol {
    readonly [Symbol.toStringTag]: string;
}
declare const console: { log(msg: any): void; };

