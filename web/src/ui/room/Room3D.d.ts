/* Room3D is JavaScript on purpose: it is a direct port of the renderer that
 * already worked, and retyping several hundred lines of scene graph would risk
 * changing behaviour while claiming only to add types. This describes its
 * surface so everything around it is still checked. */
export declare class Room3D {
  constructor(host: HTMLElement);
  setInstruments(instruments: unknown[]): void;
  setMuted(muted: Set<string>): void;
  setForced(forced: Map<string, number>): void;
  setBrightness(v: number): void;
  update(state: unknown): void;
  dispose(): void;
}
export declare function webglAvailable(): boolean;
