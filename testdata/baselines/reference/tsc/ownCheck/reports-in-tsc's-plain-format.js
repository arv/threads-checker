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

tsgo own check -p tsconfig.json --ownRuntime ./threads.mjs --pretty false
ExitStatus:: DiagnosticsPresent_OutputsSkipped
Output::
main.mts(6,3): error TS96001: 'job' cannot be used here: it was sent to 'send' on line 5.
main.mts(10,3): error TS96010: Cannot change 'table as number[]': it is a shared value, which is immutable.
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

