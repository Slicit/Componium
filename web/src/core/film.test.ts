import { describe, it, expect } from 'vitest';
import { filmForScore } from './film';

const films = [
  { name: 'sintel.mp4' },
  { name: 'big-buck-bunny.mp4' },
  { name: 'Wanted.2008.MULTi.TRUEFRENCH.1080p.BluRay.x264-FiDO.mkv' },
];

describe('filmForScore', () => {
  it('finds the film a score was named after', () => {
    expect(filmForScore('/scores/sintel.componium', films)).toBe('sintel.mp4');
  });

  it('handles a film whose name is full of dots', () => {
    /* The stem is everything up to the LAST dot, not the first, or every
     * release-named film in the library resolves to "Wanted". */
    expect(filmForScore(
      '/scores/Wanted.2008.MULTi.TRUEFRENCH.1080p.BluRay.x264-FiDO.componium', films,
    )).toBe('Wanted.2008.MULTi.TRUEFRENCH.1080p.BluRay.x264-FiDO.mkv');
  });

  it('answers empty when the film is not in the library', () => {
    expect(filmForScore('/scores/nothing.componium', films)).toBe('');
  });

  it('answers empty for a score with no path, rather than guessing', () => {
    expect(filmForScore(undefined, films)).toBe('');
    expect(filmForScore('', films)).toBe('');
  });

  it('ignores the directory the score sits in', () => {
    expect(filmForScore('sintel.componium', films)).toBe('sintel.mp4');
    expect(filmForScore(String.raw`C:\scores\sintel.componium`, films)).toBe('sintel.mp4');
  });

  it('does not match a dotfile stem to nothing', () => {
    /* ".componium" has no stem before the dot; it must not match a film whose
     * own name begins with a dot by both reducing to the empty string. */
    expect(filmForScore('/scores/.componium', [{ name: '.mp4' }])).toBe('');
  });
});
