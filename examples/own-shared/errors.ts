// Programs that the ownership analysis rejects, one per kind of error, and one that it accepts.
// Compile with `tsgo -p tsconfig.errors.json`; nbody.ts provides `worker`, `Body` and `Pos`.

// 1. Use after send, through another variable that shares objects with the sent one.
function sendHalf(bodies: sending Body[], s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  const half = bodies.slice(0, 400);
  s.spawn(worker, 0, half, board, barrier);   // half shares bodies' region: sends it
  bodies[0].x = 1;                            // error: 'bodies' shares objects with 'half', which was sent
}

// 1. Use after consume: share() turns its argument into a shared value.
function snapshot(snap: sending Pos[]) {
  const p = share(snap);
  snap[0].x = 2;                              // error: 'snap' was consumed by 'share'
  // 4. Writes through a shared value.
  p[0].x = 3;                                 // error: p[0] is shared
  p.push({ x: 0, y: 0, z: 0, m: 0 });         // error: p is shared
}

// 1. Sent in one iteration, used in the next.
function spawnInLoop(s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  const mine = makeBodies();
  for (let t = 0; t < 4; t++) {
    s.spawn(worker, t, mine, board, barrier); // error on the second iteration: 'mine' may have been sent
  }
}

// 2. Sending a value the caller still references.
function sendParameter(xs: Body[], s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  s.spawn(worker, 0, xs, board, barrier);     // error: declare xs 'sending'
}

// 2. Sending a value captured from an outer scope.
const everyone = makeBodies();
function sendCaptured(s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  s.spawn(worker, 0, everyone, board, barrier); // error: captured
}

// 3. Returning a caller's value as `sending`.
function giveBack(xs: Body[]): sending Body[] {
  return xs;                                  // error: the caller still references xs
}

// No errors: a fresh literal is sent, a map builds fresh objects that are then shared, and a value
// is only used before it is sent.
function fine(bodies: Body[], s: shared Scope, board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell) {
  s.spawn(worker, 0, [{ x: 0, y: 0, z: 0, vx: 0, vy: 0, vz: 0, m: 1 }], board, barrier);
  const positions = share(bodies.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m })));
  accelerate(bodies, positions);
  const before = makeBodies();
  move(before);
  s.spawn(worker, 1, before, board, barrier);
}
