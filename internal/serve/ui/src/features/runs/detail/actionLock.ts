export type RunAction = "interrupt" | "redrive";

export interface RunActionLock {
  active(): RunAction | null;
  tryAcquire(action: RunAction): boolean;
  release(action: RunAction): void;
}

export function createRunActionLock(): RunActionLock {
  let current: RunAction | null = null;
  return {
    active: () => current,
    tryAcquire: (action) => {
      if (current !== null) return false;
      current = action;
      return true;
    },
    release: (action) => {
      if (current === action) current = null;
    },
  };
}
