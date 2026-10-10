//// [tests/cases/compiler/ownSharedStructTopLevel.ts] ////

//// [ownSharedStructTopLevel.ts]
// A shared struct's value is initialized by its declaration, like a class: a module can use it at
// the top level (and still not before its declaration).

export const early = new Late();             // error: used before its declaration
shared struct Late { v: number }

shared struct Point { x: number; y: number }
const origin = new Point();
export const x = origin.x;


//// [ownSharedStructTopLevel.js]
// A shared struct's value is initialized by its declaration, like a class: a module can use it at
// the top level (and still not before its declaration).
export const early = new Late(); // error: used before its declaration
const Late = __sharedStruct("Late", ["v"]);
const Point = __sharedStruct("Point", ["x", "y"]);
const origin = new Point();
export const x = origin.x;
