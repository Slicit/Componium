# Repository layout

Componium is a monorepo. The realtime core is Go; the other parts are not, and
they live alongside rather than inside it.

```
cmd/componium/        CLI entrypoint: play, rehearse, spike
internal/
  clock/              TimeSource interface and the clock filter
  conductor/          scheduler, instrument registry, safety supervisor
  instrument/         CIP manifest types, the wire protocol
  score/              score parsing and serialisation
instruments/
  virtual/            virtual instruments, used by tests and by contributors
                      who have no hardware
spikes/               throwaway measurement programs; not part of the build
composer/             offline AI assisted score generation (Python)
studio/               timeline authoring UI (TypeScript, React)
firmware/             ESP32 node firmware (C, ESP-IDF)
docs/                 protocol spec and architecture decisions
LOGBOOK/              project context, features, notes
```

## One Go module

`github.com/Slicit/Componium` at the root. The Go parts are one program, so
they are one module. `composer/`, `studio/` and `firmware/` are separate
languages and carry their own toolchains.

Note the module path matches the GitHub repository exactly, including the
capital C. Go escapes uppercase in proxy paths (`github.com/!slicit/!componium`),
which works but is ugly. Renaming the repository to lowercase would avoid it,
and is easiest to do now rather than later.

## What goes in internal/ versus instruments/

`internal/` is the conductor and everything it needs to keep time. Nothing in
there may know about a specific piece of hardware.

`instruments/` is where hardware knowledge lives. An instrument translates
domain values (colour, 6DOF pose, normalised intensity) into device commands,
and is free to be ugly about it.
