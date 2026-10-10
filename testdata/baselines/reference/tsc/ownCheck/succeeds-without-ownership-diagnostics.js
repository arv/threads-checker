currentDirectory::/home/src/workspaces/project
useCaseSensitiveFileNames::true
Input::
//// [/home/src/workspaces/project/main.mts] *new* 
import { Channel } from "./threads.mjs";
export function send(ch: Channel<number[]>) {
  const data = [1, 2];
  ch.send(data);
}
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

tsgo own check --ownRuntime ./threads.mjs
ExitStatus:: Success
Output::
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

