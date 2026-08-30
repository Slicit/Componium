/* Repeating a material across a surface without distorting it.
 *
 * A photograph of a slatted panel stretched to fit a wall stops being slats:
 * the slats come out a third of a metre across and the eye reads planks, or a
 * fence, or nothing. What it has to do instead is keep its own shape and tile,
 * so a slat stays a slat and the wall simply holds more of them.
 *
 * Which means the repeat is not a number anybody picks. It falls out of the
 * two shapes, and this is where it falls out.
 */

export interface Repeat {
  x: number;
  y: number;
}

/**
 * How many times a texture repeats across a surface, at its own aspect.
 *
 * One tile is as tall as the surface, so the pattern runs the full height
 * once and repeats sideways. That is the right choice for anything with a
 * vertical grain — panelling, slats, boarding — where a seam across the
 * middle would be obvious and a seam down the side is where a real panel ends
 * anyway.
 *
 * `tall` asks for more than one tile up the surface, for a material whose
 * natural size is smaller than the wall.
 */
export function repeatForAspect(
  surfaceWidth: number,
  surfaceHeight: number,
  textureWidth: number,
  textureHeight: number,
  tall = 1,
): Repeat {
  const usable = [surfaceWidth, surfaceHeight, textureWidth, textureHeight, tall];
  if (usable.some((v) => !isFinite(v) || v <= 0)) return { x: 1, y: 1 };
  /* The width one tile takes at the surface's scale, if it stands the full
   * height of it. */
  const tileWidth = (surfaceHeight / tall) * (textureWidth / textureHeight);
  return { x: surfaceWidth / tileWidth, y: tall };
}

/**
 * The real width one tile occupies, for checking a choice against the world.
 *
 * A slatted panel whose tile works out at four metres wide is not tiling, it
 * is stretched with extra steps, and the number is the only way to notice.
 */
export function tileWidth(
  surfaceHeight: number,
  textureWidth: number,
  textureHeight: number,
  tall = 1,
): number {
  if (![surfaceHeight, textureWidth, textureHeight, tall].every((v) => isFinite(v) && v > 0)) {
    return 0;
  }
  return (surfaceHeight / tall) * (textureWidth / textureHeight);
}
