// @target: es2022
// @module: esnext
// @moduleResolution: bundler
// @strict: true
// @lib: es2022
// @ownRuntime: ./threads.mjs
// @noEmit: true

// A module's own exported functions are code, like its other functions: calling one does not keep
// its arguments, which may then be returned as `Sending<T>`. Inside the module, an exported
// function's name resolves to a local symbol that stands for the export.

// @filename: threads.d.mts
/** A value whose ownership moves, on a parameter or a result. To TypeScript it is just T. */
export type Sending<T> = T;

// @filename: bodies.mts
import type { Sending } from "./threads.mjs";

export type Body = { x: number; vx: number; m: number };
declare function makeBodies(): Sending<Body[]>;

export function move(mine: Body[]) {
  for (const b of mine) b.x += b.vx;
}
function local(mine: Body[]) {
  move(mine);
}
export class Mover {
  static move(mine: Body[]) {
    move(mine);
  }
}

export function step(): Sending<Body[]> {
  const bodies = makeBodies();
  move(bodies);
  local(bodies);
  Mover.move(bodies);
  return bodies;
}

// Still an error: the caller keeps its own array.
export function giveBack(xs: Body[]): Sending<Body[]> {
  move(xs);
  return xs;
}
