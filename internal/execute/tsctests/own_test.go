package tsctests

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/testutil/stringtestutil"
)

// `tsgo own check`: the ownership checker of the own/shared threads extension, on programs in plain
// TypeScript (the markers of the runtime module that `--ownRuntime` names).
func TestOwnCheck(t *testing.T) {
	t.Parallel()
	threads := stringtestutil.Dedent(`
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
		}`)
	tsconfig := stringtestutil.Dedent(`
		{
		  "compilerOptions": { "target": "es2022", "module": "esnext", "moduleResolution": "bundler", "strict": true, "noEmit": true },
		  "files": ["main.mts"]
		}`)
	mistakes := stringtestutil.Dedent(`
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
		const notANumber: number = "a type error, which is tsc's to report";`)
	clean := stringtestutil.Dedent(`
		import { Channel } from "./threads.mjs";
		export function send(ch: Channel<number[]>) {
		  const data = [1, 2];
		  ch.send(data);
		}`)
	testCases := []*tscInput{
		{
			subScenario: "reports ownership diagnostics only",
			files: FileMap{
				"/home/src/workspaces/project/threads.d.mts": threads,
				"/home/src/workspaces/project/main.mts":      mistakes,
				"/home/src/workspaces/project/tsconfig.json": tsconfig,
			},
			commandLineArgs: []string{"own", "check", "-p", "tsconfig.json", "--ownRuntime", "./threads.mjs"},
		},
		{
			subScenario: "reports in tsc's plain format",
			files: FileMap{
				"/home/src/workspaces/project/threads.d.mts": threads,
				"/home/src/workspaces/project/main.mts":      mistakes,
				"/home/src/workspaces/project/tsconfig.json": tsconfig,
			},
			commandLineArgs: []string{"own", "check", "-p", "tsconfig.json", "--ownRuntime", "./threads.mjs", "--pretty", "false"},
		},
		{
			subScenario: "succeeds without ownership diagnostics",
			files: FileMap{
				"/home/src/workspaces/project/threads.d.mts": threads,
				"/home/src/workspaces/project/main.mts":      clean,
				"/home/src/workspaces/project/tsconfig.json": tsconfig,
			},
			commandLineArgs: []string{"own", "check", "--ownRuntime", "./threads.mjs"},
		},
		{
			subScenario: "requires ownRuntime",
			files: FileMap{
				"/home/src/workspaces/project/threads.d.mts": threads,
				"/home/src/workspaces/project/main.mts":      clean,
				"/home/src/workspaces/project/tsconfig.json": tsconfig,
			},
			commandLineArgs: []string{"own", "check", "-p", "tsconfig.json"},
		},
		{
			subScenario:     "shows usage",
			commandLineArgs: []string{"own"},
		},
	}
	for _, test := range testCases {
		test.run(t, "ownCheck")
	}
}
