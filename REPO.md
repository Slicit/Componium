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

The module path is lowercase, `github.com/Slicit/componium`, which is what
Go proxies and import lines want. GitHub repository names are case
insensitive for fetching, so this resolves whether or not the repository
itself has been renamed, and it avoids the `!componium` proxy escaping that
an uppercase path produces.

## What goes in internal/ versus instruments/

`internal/` is the conductor and everything it needs to keep time. Nothing in
there may know about a specific piece of hardware.

`instruments/` is where hardware knowledge lives. An instrument translates
domain values (colour, 6DOF pose, normalised intensity) into device commands,
and is free to be ugly about it.
