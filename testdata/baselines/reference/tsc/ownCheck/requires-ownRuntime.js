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

tsgo own check -p tsconfig.json
ExitStatus:: InvalidProject_OutputsSkipped
Output::
tsgo own check: no ownRuntime. Give the module programs import the runtime from: --ownRuntime ./threads.mjs

