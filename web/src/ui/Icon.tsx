/* Icons, for the actions that have one.
 *
 * The rule, and it is a rule rather than a preference: an action with a
 * settled, universal icon gets the icon, never the word. Delete is a bin, save
 * is a disk, edit is a pencil — everybody already knows those, and spelling
 * them out costs a row of width to tell people something they can see faster.
 *
 * The other half of the rule matters more: an action WITHOUT a settled icon
 * gets a word. Rebuild, Prepare, Reset and Resume have no glyph anyone would
 * read the same way twice, and inventing one produces a row of little symbols
 * that all have to be hovered to be understood. A mixed row of icons and words
 * is not an inconsistency, it is the point — the icons are the actions you do
 * often and recognise instantly, the words are the ones worth reading.
 *
 * Drawn inline rather than pulled from a font or a package: five glyphs is not
 * worth a dependency, and an icon font that fails to load leaves squares.
 */

export type IconName =
  | 'trash' | 'save' | 'edit' | 'search' | 'left' | 'right' | 'play' | 'stop';

const paths: Record<IconName, JSX.Element> = {
  /* A bin with a lid and two staves. */
  trash: (
    <>
      <path d="M3 5h14M8 5V3.5A1.5 1.5 0 0 1 9.5 2h1A1.5 1.5 0 0 1 12 3.5V5" />
      <path d="M5 5l1 11.5A1.5 1.5 0 0 0 7.5 18h5a1.5 1.5 0 0 0 1.5-1.5L15 5" />
      <path d="M8.5 8.5v6M11.5 8.5v6" />
    </>
  ),
  /* A floppy disk, which stopped being a physical object long ago and never
   * stopped meaning save. */
  save: (
    <>
      <path d="M4 3h9l4 4v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" />
      <path d="M6.5 3v5h7V3" />
      <path d="M6.5 18v-5h7v5" />
    </>
  ),
  edit: (
    <>
      <path d="M14.5 3.5a1.6 1.6 0 0 1 2.3 2.3L7.6 15H5v-2.6z" />
      <path d="M3 18h14" />
    </>
  ),
  search: (
    <>
      <circle cx="9" cy="9" r="5.5" />
      <path d="M13 13l4 4" />
    </>
  ),
  /* Play and stop are as settled as delete is, so they are icons too. */
  play: <path d="M6.5 4.2l9 5.8-9 5.8z" />,
  stop: <rect x="5.5" y="5.5" width="9" height="9" rx="1" />,
  left: <path d="M12.5 4L7 10l5.5 6" />,
  right: <path d="M7.5 4l5.5 6-5.5 6" />,
};

/**
 * One icon, sized to the text around it.
 *
 * Hidden from screen readers, because an icon button carries its meaning in
 * its label — announcing "bin, Delete sintel.mp4" says it twice.
 */
export function Icon(props: { name: IconName; size?: number }) {
  return (
    <svg
      className="icon"
      viewBox="0 0 20 20"
      width={props.size ?? 14}
      height={props.size ?? 14}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {paths[props.name]}
    </svg>
  );
}
