## What this changes

<!-- One or two sentences. The why matters more than the what. -->

## How it was verified

<!-- Tests, and anything you ran by hand. If you tested against real hardware,
say which, and say what the declared latency ended up being: those numbers are
currently guesses everywhere they appear. -->

## Checklist

- [ ] I have signed the CLA (see [CLA/README.md](../CLA/README.md)). The bot
      will tell you if not.
- [ ] `go test ./...` passes
- [ ] `gofmt -l .` and `go vet ./...` are clean
- [ ] Anything that can hurt someone declares its limits rather than defaulting
- [ ] Non-obvious decisions are recorded in `LOGBOOK/features/`
