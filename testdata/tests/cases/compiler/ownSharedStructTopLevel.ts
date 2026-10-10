// @target: es2022
// @module: esnext
// @strict: true

// A shared struct's value is initialized by its declaration, like a class: a module can use it at
// the top level (and still not before its declaration).

export const early = new Late();             // error: used before its declaration
shared struct Late { v: number }

shared struct Point { x: number; y: number }
const origin = new Point();
export const x = origin.x;
