type Body = { x: number; y: number; z: number; vx: number; vy: number; vz: number; m: number };
type Pos = { x: number; y: number; z: number; m: number };
declare const STEPS: number;
declare function makeBodies(): sending Body[];
declare function accelerate(mine: Body[], all: readonly shared Pos[]): void;
declare function move(mine: Body[]): void;
declare function energy(bodies: Body[]): number;

function worker(t: number, mine: sending Body[], board: shared SlotsCell<Pos[]>, barrier: shared BarrierCell): sending Body[] {
  for (let s = 0; s < STEPS; s++) {
    board.set(t, share(mine.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m }))));
    barrier.wait();
    accelerate(mine, board.flat());
    barrier.wait();
    move(mine);
  }
  return mine;
}

function simulate(threads: number): number {
  const board = new Slots<Pos[]>(threads), barrier = new Barrier(threads);
  return scope(s => {
    const parts = partition(makeBodies(), threads);
    const ts: (shared Thread<Body[]>)[] = [];
    for (let t = 0; t < threads; t++) {
      ts.push(s.spawn(worker, t, take(parts, t), board, barrier));
    }
    return energy(ts.flatMap(th => th.join()));
  });
}

shared struct Counter { n: number; total: number }
